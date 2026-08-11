package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/romanvolkov650/go-steam"
)

func main() {
	cookiesFile := "cookies_steamuhd.json"
	if _, err := os.Stat(cookiesFile); os.IsNotExist(err) {
		// Try using a copy from active location if you copied it back
		cookiesFile = "/Users/romanvolkov/go-steam/cookies_steamuhd.json"
		if _, err := os.Stat(cookiesFile); os.IsNotExist(err) {
			log.Fatalf("No cookies file found to test logout.")
		}
	}

	proxyURL := os.Getenv("STEAM_PROXY")
	client, err := steam.NewClient(steam.ClientConfig{
		SteamID:  "76561199861735244",
		ProxyURL: proxyURL,
	})
	if err != nil {
		log.Fatalf("NewClient failed: %v", err)
	}

	if err := client.LoadCookiesFromFile(cookiesFile); err != nil {
		log.Fatalf("LoadCookiesFromFile failed: %v", err)
	}

	fmt.Println("Verifying active session...")
	alive, err := client.IsSessionAlive()
	if err != nil {
		log.Fatalf("Check failed: %v", err)
	}
	fmt.Printf("Session Alive before Logout: %v\n", alive)

	if !alive {
		return
	}

	fmt.Println("Performing updated steamcommunity.com Logout...")
	if err := client.LogoutWithContext(context.Background()); err != nil {
		log.Fatalf("LogoutWithContext failed: %v", err)
	}

	fmt.Println("Logout triggered successfully! Checking status again...")
	aliveAfter, err := client.IsSessionAlive()
	fmt.Printf("Session Alive after Logout: %v (err: %v)\n", aliveAfter, err)
}
