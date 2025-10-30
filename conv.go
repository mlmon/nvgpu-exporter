package main

// pciBusIdToString converts a PCI bus ID byte array to a human-readable string
// Standard PCI address format is: DDDD:BB:DD.F (e.g., 0000:00:1e.0)
// This is typically 12-13 characters long
func pciBusIdToString(busId [16]uint8) string {
	// Standard PCI address is domain:bus:device.function (12-13 chars)
	// Find the end by looking for common PCI address length
	str := string(busId[:])
	// Find the last digit or period in the expected PCI format
	for i := 12; i < len(busId) && i < 14; i++ {
		if busId[i] == 0 || busId[i] < 32 || busId[i] > 126 {
			return str[:i]
		}
	}
	return str[:13]
}
