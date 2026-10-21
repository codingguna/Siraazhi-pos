/* ==========================================================================
   Package printer — Network ESC/POS Thermal Printer Driver (TCP/IP Port 9100)
   
   WHAT IT IS:
     Direct raw TCP socket client for network-attached receipt printers.
     Connects to Ethernet/Wi-Fi thermal printers over LAN with zero drivers.
   ========================================================================== */

package printer

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

// SendRawToNetwork writes raw byte stream to a network printer at ip:port
func SendRawToNetwork(ip string, port int, data []byte) error {
	if ip == "" {
		return fmt.Errorf("network printer IP address is required")
	}
	if port <= 0 {
		port = 9100 // Standard JetDirect raw printing port
	}

	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("could not connect to network printer at %s: %w", addr, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send print data to %s: %w", addr, err)
	}

	return nil
}
