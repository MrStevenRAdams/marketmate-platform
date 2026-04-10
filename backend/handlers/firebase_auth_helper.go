package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// firebaseAuthSendPasswordReset sends a password-reset email using Firebase Auth REST API.
// Requires env var FIREBASE_WEB_API_KEY.
func firebaseAuthSendPasswordReset(ctx context.Context, email string) error {
	apiKey := os.Getenv("FIREBASE_WEB_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("VITE_FIREBASE_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("FIREBASE_WEB_API_KEY environment variable not set")
	}

	url := fmt.Sprintf(
		"https://identitytoolkit.googleapis.com/v1/accounts:sendOobCode?key=%s",
		apiKey,
	)

	payload := map[string]string{
		"requestType": "PASSWORD_RESET",
		"email":       email,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("firebase request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("firebase error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
