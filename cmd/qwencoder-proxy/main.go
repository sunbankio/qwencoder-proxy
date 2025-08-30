package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"qwenproxy/auth"
	"qwenproxy/proxy"
	"qwenproxy/qwenclient"
)

func main() {
	// Set up the HTTP handler for /v1/models
	http.HandleFunc("/v1/models", proxy.ModelsHandler)

	// Set up the general proxy handler for all other routes
	http.HandleFunc("/", proxy.ProxyHandler)

	// Check for credentials on startup
	log.Println("Checking Qwen credentials...")
	_, _, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "credentials not found") || strings.Contains(errorMsg, "failed to refresh token") {
			log.Println("Credentials not found or invalid. Initiating authentication flow...")
			// Ensure the credentials file is removed before attempting authentication
			credsPath := auth.GetQwenCredentialsPath()
			if _, fileErr := os.Stat(credsPath); fileErr == nil {
				if removeErr := os.Remove(credsPath); removeErr != nil {
					log.Printf("Failed to remove existing credentials file %s: %v", credsPath, removeErr)
				} else {
					log.Printf("Successfully removed existing credentials file: %s", credsPath)
				}
			}

			authErr := auth.AuthenticateWithOAuth()
			if authErr != nil {
				log.Fatalf("Authentication failed during startup: %v", authErr)
			}
			log.Println("Authentication successful. Starting proxy server...")
		} else {
			log.Fatalf("Failed to check credentials on startup: %v", err)
		}
	} else {
		log.Println("Credentials found and valid. Starting proxy server...")
	}

	// Start the server
	fmt.Printf("Proxy server starting on port %s\n", auth.Port)
	if err := http.ListenAndServe(":"+auth.Port, nil); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}