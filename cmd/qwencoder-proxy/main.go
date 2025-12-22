package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/sunbankio/qwencoder-proxy/auth"
	"github.com/sunbankio/qwencoder-proxy/config"
	"github.com/sunbankio/qwencoder-proxy/converter"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/proxy"
	"github.com/sunbankio/qwencoder-proxy/provider"
	"github.com/sunbankio/qwencoder-proxy/provider/antigravity"
	"github.com/sunbankio/qwencoder-proxy/provider/gemini"
	"github.com/sunbankio/qwencoder-proxy/provider/kiro"
	"github.com/sunbankio/qwencoder-proxy/provider/qwen"
	"github.com/sunbankio/qwencoder-proxy/qwenclient"
)

func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Define the debug flag
	var debugFlag bool
	flag.BoolVar(&debugFlag, "debug", cfg.Logging.IsDebugMode, "Enable debug mode for verbose logging")
	flag.Parse()

	// Set the global debug mode variable
	logging.IsDebugMode = debugFlag

	// Create provider factory and register providers
	factory := provider.NewFactory()

	// Register Qwen provider (existing)
	qwenProvider := qwen.NewProvider()
	factory.Register(qwenProvider)
	
	// Register Gemini provider
	geminiAuth := auth.NewGeminiAuthenticator(nil)
	geminiProvider := gemini.NewProvider(geminiAuth)
	factory.Register(geminiProvider)

	// Register Kiro provider
	kiroAuth := auth.NewKiroAuthenticator(nil)
	kiroProvider := kiro.NewProvider(kiroAuth)
	factory.Register(kiroProvider)

	// Register Antigravity provider
	antigravityAuth := auth.NewGeminiAuthenticator(&auth.GeminiOAuthConfig{
		ClientID:     "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
		ClientSecret: "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",
		Scope:        "https://www.googleapis.com/auth/cloud-platform",
		RedirectPort: 8086,
		CredsDir:     ".antigravity",
		CredsFile:    "oauth_creds.json",
	})
	antigravityProvider := antigravity.NewProvider(antigravityAuth)
	factory.Register(antigravityProvider)

	// Create converter factory
	convFactory := converter.NewFactory()

	// Register native format routes
	proxy.RegisterGeminiRoutes(http.DefaultServeMux, factory)
	proxy.RegisterAnthropicRoutes(http.DefaultServeMux, factory)

	// Register OpenAI-compatible routes
	proxy.RegisterOpenAIRoutes(http.DefaultServeMux, factory, convFactory)

	// Set up the general proxy handler for all other routes (fallback)
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
	debugStatus := ""
	if logging.IsDebugMode {
		debugStatus = " [DEBUG ON]"
	}
	fmt.Printf("Proxy server starting on port %s%s\n", cfg.Server.Port, debugStatus)
	if err := http.ListenAndServe(":"+cfg.Server.Port, nil); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}