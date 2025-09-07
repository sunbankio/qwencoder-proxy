package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
	"golang.org/x/oauth2"
)

// AuthenticateWithDeviceFlow handles the OAuth 2.0 device authorization flow using the golang.org/x/oauth2 package.
func AuthenticateWithDeviceFlow() error {
	conf := &oauth2.Config{
		ClientID: QwenOAuthClientID,
		Scopes:   []string{QwenOAuthScope},
		Endpoint: oauth2.Endpoint{
			TokenURL:      QwenOAuthTokenURL,
			DeviceAuthURL: QwenOAuthDeviceAuthURL,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return fmt.Errorf("failed to generate code verifier: %w", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)

	deviceAuthResponse, err := conf.DeviceAuth(ctx,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	if err != nil {
		return fmt.Errorf("failed to start device auth flow: %w", err)
	}

	// Construct verification URL with user code and client parameter
	// Use "qwen-code" as the client parameter value
	var verificationURL string
	if deviceAuthResponse.VerificationURIComplete != "" {
		verificationURL = deviceAuthResponse.VerificationURIComplete
	} else {
		verificationURL = fmt.Sprintf("%s?user_code=%s&client=qwen-code", deviceAuthResponse.VerificationURI, deviceAuthResponse.UserCode)
	}

	// Try to open the verification URI in the browser
	if err := openBrowser(verificationURL); err != nil {
		logging.NewLogger().WarningLog("Failed to open browser automatically: %v. Please open the URL manually.", err)
	}

	fmt.Printf("\n=== Qwen OAuth Authentication ===\n")
	fmt.Printf("If your browser didn't open, please go to: %s\n", verificationURL)
	fmt.Printf("And enter this code: %s\n\n", deviceAuthResponse.UserCode)
	fmt.Println("Waiting for authorization...")

	token, err := conf.DeviceAccessToken(ctx, deviceAuthResponse, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	creds := OAuthCreds{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		ExpiryDate:   token.Expiry.UnixMilli(),
	}
	if resourceURL, ok := token.Extra("resource_url").(string); ok {
		creds.ResourceURL = resourceURL
	}

	if err := SaveOAuthCreds(creds); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Println("Authentication successful! Credentials saved.")
	return nil
}

// openBrowser opens the default browser with the given URL.
func openBrowser(url string) error {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	return err
}

// generateCodeVerifier generates a random code verifier for PKCE.
func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// generateCodeChallenge generates a code challenge from a code verifier using SHA-256.
func generateCodeChallenge(codeVerifier string) string {
	h := sha256.New()
	h.Write([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
