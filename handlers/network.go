/* ==========================================================================
   Package handlers — Local Network Discovery & QR Code Endpoint
   
   WHAT IT IS:
     This module enables zero-configuration multi-device discovery over local Wi-Fi:
     - Discovers active Wi-Fi (Wi-Fi, Wi-Fi 2, WLAN) and Ethernet network adapters.
     - Ignores virtual adapters (WSL, Hyper-V, VirtualBox, Bluetooth, 169.254 APIPA).
     - Constructs client connect URLs (e.g., http://10.98.92.48:8080).
     - Allows staff on the shop floor to scan a QR code with their phone camera to
       instantly launch the Siraazhi Staff Billing terminal on their mobile device.

   HOW IT WORKS:
     - Inspects all system network interfaces via net.Interfaces().
     - Filters specifically for Wi-Fi and Ethernet adapters with active IPv4.
     - Assembles formatted URLs with the active server port (default 8080).
   ========================================================================== */

package handlers

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// NetworkInfo provides IP addresses for other devices to connect
type NetworkInfo struct {
	Port int      `json:"port"`
	URLs []string `json:"urls"`
	IPs  []string `json:"ips"`
}

// HandleNetworkInfo returns active local IP addresses for Wi-Fi or Ethernet terminal connection.
// It specifically isolates Wi-Fi (e.g. Wi-Fi 2) or physical Ethernet connections.
func HandleNetworkInfo(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		info := NetworkInfo{
			Port: port,
		}

		ifaces, err := net.Interfaces()
		if err == nil {
			for _, iface := range ifaces {
				// Must be up and not loopback
				if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
					continue
				}

				name := strings.ToLower(iface.Name)
				// Check for Wi-Fi or physical Ethernet
				isWiFi := strings.Contains(name, "wi-fi") || strings.Contains(name, "wifi") || strings.Contains(name, "wlan") || strings.Contains(name, "wireless")
				isEthernet := (strings.Contains(name, "ethernet") || strings.Contains(name, "eth")) && !strings.Contains(name, "vethernet")

				// Exclude virtual adapters, WSL, Hyper-V, VMware, VirtualBox, Bluetooth, asterisks
				if strings.Contains(name, "virtual") || strings.Contains(name, "vethernet") ||
					strings.Contains(name, "wsl") || strings.Contains(name, "bluetooth") ||
					strings.Contains(name, "vmware") || strings.Contains(name, "*") {
					continue
				}

				if !isWiFi && !isEthernet {
					continue
				}

				addrs, err := iface.Addrs()
				if err != nil {
					continue
				}

				for _, addr := range addrs {
					if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
						ip4 := ipNet.IP.To4()
						if ip4 != nil {
							ipStr := ip4.String()
							// Skip APIPA link-local 169.254.x.x
							if strings.HasPrefix(ipStr, "169.254.") {
								continue
							}
							info.IPs = append(info.IPs, ipStr)
							info.URLs = append(info.URLs, "http://"+ipStr+":"+strconv.Itoa(port))
						}
					}
				}
			}
		}

		// Fallback: If no interface matched, inspect all non-loopback IPv4
		if len(info.IPs) == 0 {
			addrs, err := net.InterfaceAddrs()
			if err == nil {
				for _, addr := range addrs {
					if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
						ip4 := ipNet.IP.To4()
						if ip4 != nil {
							ipStr := ip4.String()
							if !strings.HasPrefix(ipStr, "169.254.") {
								info.IPs = append(info.IPs, ipStr)
								info.URLs = append(info.URLs, "http://"+ipStr+":"+strconv.Itoa(port))
							}
						}
					}
				}
			}
		}

		JSON(w, http.StatusOK, info)
	}
}
