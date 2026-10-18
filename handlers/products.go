/* ==========================================================================
   Package handlers — Product Catalog & Barcode Scanner API
   
   WHAT IT IS:
     This module manages the product inventory catalog:
     - Listing all active products and prices.
     - Creating new products with English and Tamil names, HSN codes, and variants.
     - Updating existing prices, MRP, and stock alert levels.
     - Deleting discontinued products.
     - Instant barcode lookup for hardware USB/Bluetooth barcode scanners.

   HOW IT WORKS:
     - HandleProducts():
         GET: Retrieves all products from SQLite sorted by category and volume.
         POST: Validates required name and price fields, then upserts the product.
     - HandleProductByID():
         GET: Fetches a single product by its unique alphanumeric ID.
         DELETE: Removes a product from the catalog table.
     - HandleProductByBarcode():
         GET: Performs an indexed search on the barcode column. When a barcode scanner
         reads an EAN/UPC code at the counter, this endpoint returns the product instantly
         to populate the cart in under 5 milliseconds.

   FLOW:
     Client Request -> Route Match -> Parameter Validation -> SQLite DB Query -> JSON Response
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"net/http"
	"siraazhi-pos/db"
	"strings"
)

// HandleProducts handles GET /api/products and POST /api/products.
// Flow: GET queries all products -> encodes to JSON. POST decodes body -> validates fields -> saves to DB.
func HandleProducts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		products, err := db.GetProducts()
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to fetch products: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, products)

	case http.MethodPost:
		var p db.Product
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid product payload")
			return
		}
		if strings.TrimSpace(p.NameEn) == "" || p.Price < 0 {
			ErrorJSON(w, http.StatusBadRequest, "Name (English) and non-negative price are required")
			return
		}
		if p.MRP < p.Price {
			p.MRP = p.Price
		}
		if p.Unit == "" {
			p.Unit = "Bottle"
		}
		if err := db.SaveProduct(p); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to save product: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{"ok": true, "product": p})

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleProductByID handles GET/DELETE /api/products/{id}.
// Flow: Extracts ID from URL path -> runs DB query or delete -> returns product or success boolean.
func HandleProductByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/products/")
	if id == "" {
		ErrorJSON(w, http.StatusBadRequest, "Missing product ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		p, err := db.GetProductByID(id)
		if err != nil {
			ErrorJSON(w, http.StatusNotFound, "Product not found")
			return
		}
		JSON(w, http.StatusOK, p)

	case http.MethodDelete:
		if err := db.DeleteProduct(id); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to delete product: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleProductByBarcode handles GET /api/products/barcode/{code}.
// Flow: Extracts barcode from path -> queries products table by barcode -> returns matching product for POS cart.
func HandleProductByBarcode(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/api/products/barcode/")
	if code == "" {
		ErrorJSON(w, http.StatusBadRequest, "Missing barcode")
		return
	}
	p, err := db.GetProductByBarcode(code)
	if err != nil {
		ErrorJSON(w, http.StatusNotFound, "Product with barcode not found")
		return
	}
	JSON(w, http.StatusOK, p)
}
