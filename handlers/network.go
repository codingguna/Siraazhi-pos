/* ==========================================================================
   Package handlers — Local Network Discovery & QR Code Endpoint
   
   WHAT IT IS:
     This module enables zero-configuration multi-device discovery over local Wi-Fi:
     - Discovers all active IPv4 network interface addresses on the host PC.
     - Constructs client connect URLs (e.g., http://192.168.1.15:8080).
     - Allows staff on the shop floor to scan a QR code with their phone camera to
       instantly launch the Siraazhi Staff Billing terminal on their mobile device.

   HOW IT WORKS:
     - HandleNetworkInfo(port):
         1. Invokes Go's net.InterfaceAddrs() to inspect all system network adapters.
         2. Filters out loopback addresses (127.0.0.1) and IPv6 link-local addresses.
         3. Extracts valid IPv4 LAN addresses assigned by the shop's Wi-Fi router.
         4. Assembles formatted URLs with the active server port (e.g. 8080).
         5. Returns JSON payload consumed by the frontend QR code generator.

   FLOW:
     Client calls /api/network-info -> net.InterfaceAddrs() -> IPv4 filtering -> JSON URL list
   ========================================================================== */

package handlers

import (
	"net"
	"net/http"
	"strconv"
)

// NetworkInfo provides IP addresses for other devices to connect
type NetworkInfo struct {
	Port int      `json:"port"`
	URLs []string `json:"urls"`
	IPs  []string `json:"ips"`
}

// HandleNetworkInfo returns all active local IP addresses for Wi-Fi terminal connection.
// Flow: scans host network interfaces -> filters IPv4 -> attaches active port -> returns JSON.
func HandleNetworkInfo(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		info := NetworkInfo{
			Port: port,
		}

		addrs, err := net.InterfaceAddrs()
		if err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
					if ipNet.IP.To4() != nil {
						ipStr := ipNet.IP.String()
						info.IPs = append(info.IPs, ipStr)
						info.URLs = append(info.URLs, "http://"+ipStr+":"+strconv.Itoa(port))
					}
				}
			}
		}

		// Include localhost for reference
		info.URLs = append(info.URLs, "http://localhost:"+strconv.Itoa(port))

		JSON(w, http.StatusOK, info)
	}
}
