/* ==========================================================================
   Package handlers — Product Category REST Controller
   
   WHAT IT IS:
     Enables dynamic product categorization and merchant category additions:
     - GET  /api/categories      -> Returns list of all categories (both system & custom)
     - POST /api/categories      -> Creates or updates a custom category
     - DELETE /api/categories/{id} -> Removes a custom category
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"net/http"
	"siraazhi-pos/db"
	"strings"
)

// HandleCategories handles category listing and creation.
func HandleCategories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cats, err := db.GetCategories()
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to load categories: "+err.Error())
			return
		}
		if cats == nil {
			cats = []db.Category{}
		}
		JSON(w, http.StatusOK, cats)

	case http.MethodPost:
		var c db.Category
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(c.Name) == "" {
			ErrorJSON(w, http.StatusBadRequest, "Category name is required")
			return
		}
		if err := db.SaveCategory(c); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to save category: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, map[string]string{"message": "Category saved successfully"})

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleCategoryDelete removes a custom category.
func HandleCategoryDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	// Path pattern: /api/categories/xyz
	id := strings.TrimPrefix(r.URL.Path, "/api/categories/")
	id = strings.TrimSpace(id)
	if id == "" {
		ErrorJSON(w, http.StatusBadRequest, "Category ID or name is required")
		return
	}
	if err := db.DeleteCategory(id); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to delete category: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"message": "Category deleted successfully"})
}
