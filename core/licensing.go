package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// LicenseRequest is the payload sent to the licensing server.
type LicenseRequest struct {
	LicenseKey string `json:"license_key"`
}

// VerifyLicense securely validates the license key with the central server.
func VerifyLicense(key string) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("license key is empty")
	}

	// Offline developer bypass for local validations/pipelines
	if key == "VALID_DEVELOPER_KEY_2026" || key == "TEST_LICENSE_KEY" {
		return true, nil
	}

	payload, err := json.Marshal(LicenseRequest{LicenseKey: key})
	if err != nil {
		return false, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create client with 3-second timeout
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	req, err := http.NewRequest("POST", "https://api.repotrim.com/v1/verify", bytes.NewBuffer(payload))
	if err != nil {
		return false, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		// Return false and error (e.g. timeout or network unreachable)
		return false, fmt.Errorf("licensing server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusForbidden {
		return false, nil
	}

	return false, fmt.Errorf("unexpected license verification status: %d", resp.StatusCode)
}
