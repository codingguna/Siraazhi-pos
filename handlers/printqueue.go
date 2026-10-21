/* ==========================================================================
   Package handlers — Master System Print Queue Manager
   
   WHAT IT IS:
     Central server-side print queue for multi-device retail operations:
     - Allows shop floor mobile phones and tablets to submit print jobs to the Master PC.
     - Physical thermal receipt printers (80mm/58mm USB/LAN) are connected to the Master System.
     - Prevents client mobile devices from attempting local browser window.print().
     - Feeds an automated or 1-click spooler on the Master System desktop terminal.

   HOW IT WORKS:
     1. Client terminals on Wi-Fi call POST /api/print-queue upon bill creation.
     2. Job is registered with timestamp, client IP, bill details, and target format.
     3. Master System polls GET /api/print-queue?status=pending every 2.5 seconds.
     4. When detected, Master System triggers the physical thermal printer and marks the
        job as complete via POST /api/print-queue/{id}/complete.
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"siraazhi-pos/db"
	"siraazhi-pos/printer"
)

// PrintJob represents a single invoice queued for physical hardware printing.
type PrintJob struct {
	ID        string     `json:"id"`
	BillID    string     `json:"billId"`
	BillNo    string     `json:"billNo"`
	Format    string     `json:"format"` // "80mm", "58mm", "a4", "a5"
	Status    string     `json:"status"` // "pending", "printing", "printed", "failed"
	CreatedAt time.Time  `json:"createdAt"`
	PrintedAt *time.Time `json:"printedAt,omitempty"`
	ClientIP  string     `json:"clientIp"`
	Bill      *db.Bill   `json:"bill,omitempty"`
}

// PrintQueueManager manages thread-safe access to the print queue.
type PrintQueueManager struct {
	mu   sync.RWMutex
	jobs []*PrintJob
}

var globalPrintQueue = &PrintQueueManager{
	jobs: make([]*PrintJob, 0),
}

// Enqueue adds a new print job to the queue.
func (pq *PrintQueueManager) Enqueue(bill *db.Bill, format, clientIP string) *PrintJob {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if format == "" {
		format = "80mm"
	}

	jobID := db.GenID("pj")
	job := &PrintJob{
		ID:        jobID,
		BillID:    bill.ID,
		BillNo:    bill.BillNo,
		Format:    format,
		Status:    "pending",
		CreatedAt: time.Now(),
		ClientIP:  clientIP,
		Bill:      bill,
	}

	// Prepend so newest appears first
	pq.jobs = append([]*PrintJob{job}, pq.jobs...)

	// Keep max 100 historical jobs in memory
	if len(pq.jobs) > 100 {
		pq.jobs = pq.jobs[:100]
	}

	return job
}

// GetPending returns all pending print jobs awaiting the master printer.
func (pq *PrintQueueManager) GetPending() []*PrintJob {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	pending := make([]*PrintJob, 0)
	for _, j := range pq.jobs {
		if j.Status == "pending" {
			pending = append(pending, j)
		}
	}
	return pending
}

// GetAll returns recent print jobs up to the specified limit.
func (pq *PrintQueueManager) GetAll(limit int) []*PrintJob {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	if limit <= 0 || limit > len(pq.jobs) {
		limit = len(pq.jobs)
	}

	result := make([]*PrintJob, limit)
	copy(result, pq.jobs[:limit])
	return result
}

// MarkCompleted marks a print job as successfully printed.
func (pq *PrintQueueManager) MarkCompleted(jobID string) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	now := time.Now()
	for _, j := range pq.jobs {
		if j.ID == jobID {
			j.Status = "printed"
			j.PrintedAt = &now
			return true
		}
	}
	return false
}

// Delete removes a job from the queue.
func (pq *PrintQueueManager) Delete(jobID string) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for i, j := range pq.jobs {
		if j.ID == jobID {
			pq.jobs = append(pq.jobs[:i], pq.jobs[i+1:]...)
			return true
		}
	}
	return false
}

// ClearFinished removes all printed or failed jobs.
func (pq *PrintQueueManager) ClearFinished() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	retained := make([]*PrintJob, 0)
	removed := 0
	for _, j := range pq.jobs {
		if j.Status == "pending" {
			retained = append(retained, j)
		} else {
			removed++
		}
	}
	pq.jobs = retained
	return removed
}

// HandlePrintQueue processes GET and POST for the master print queue.
func HandlePrintQueue(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := r.URL.Query().Get("status")
		if status == "pending" {
			pending := globalPrintQueue.GetPending()
			JSON(w, http.StatusOK, map[string]interface{}{
				"jobs":         pending,
				"pendingCount": len(pending),
			})
			return
		}

		jobs := globalPrintQueue.GetAll(50)
		pendingCount := len(globalPrintQueue.GetPending())
		JSON(w, http.StatusOK, map[string]interface{}{
			"jobs":         jobs,
			"pendingCount": pendingCount,
		})

	case http.MethodPost:
		var req struct {
			BillID string   `json:"billId"`
			Format string   `json:"format"`
			Bill   *db.Bill `json:"bill"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}

		targetBill := req.Bill
		if targetBill == nil || targetBill.ID == "" {
			if req.BillID == "" {
				ErrorJSON(w, http.StatusBadRequest, "billId or bill object required")
				return
			}
			loadedBill, err := db.GetBillByID(req.BillID)
			if err != nil {
				ErrorJSON(w, http.StatusNotFound, "Bill not found: "+err.Error())
				return
			}
			targetBill = &loadedBill
		}

		// Extract client IP
		clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			clientIP = r.RemoteAddr
		}

		job := globalPrintQueue.Enqueue(targetBill, req.Format, clientIP)

		// Check if direct hardware printing is configured
		settings, errSettings := db.GetSettings()
		directPrinted := false
		if errSettings == nil && printer.IsHardwarePrintingActive(&settings) && settings.AutoPrintMobile {
			res := printer.PrintBillToHardware(targetBill, req.Format, &settings)
			if res.Success && res.DirectPrint {
				globalPrintQueue.MarkCompleted(job.ID)
				directPrinted = true
			}
		}

		JSON(w, http.StatusCreated, map[string]interface{}{
			"success":       true,
			"directPrinted": directPrinted,
			"message":       "Bill processed by Master Print Queue",
			"job":           job,
		})

	case http.MethodDelete:
		removed := globalPrintQueue.ClearFinished()
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"removed": removed,
		})

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandlePrintJobAction handles actions on individual print jobs:
// POST /api/print-queue/{id}/complete
// DELETE /api/print-queue/{id}
func HandlePrintJobAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/print-queue/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		ErrorJSON(w, http.StatusBadRequest, "Missing job ID")
		return
	}

	jobID := parts[0]

	if len(parts) >= 2 && parts[1] == "complete" && r.Method == http.MethodPost {
		if ok := globalPrintQueue.MarkCompleted(jobID); !ok {
			ErrorJSON(w, http.StatusNotFound, "Print job not found")
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"jobId":   jobID,
			"status":  "printed",
		})
		return
	}

	if r.Method == http.MethodDelete {
		if ok := globalPrintQueue.Delete(jobID); !ok {
			ErrorJSON(w, http.StatusNotFound, "Print job not found")
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"deleted": jobID,
		})
		return
	}

	ErrorJSON(w, http.StatusNotFound, "Unknown print queue action")
}
