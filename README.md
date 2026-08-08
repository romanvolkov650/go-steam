# go-steam

[![Go Reference](https://pkg.go.dev/badge/github.com/romanvolkov650/go-steam.svg)](https://pkg.go.dev/github.com/romanvolkov650/go-steam)
[![Go Report Card](https://goreportcard.com/badge/github.com/romanvolkov650/go-steam)](https://goreportcard.com/report/github.com/romanvolkov650/go-steam)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

`go-steam` is a high-performance, feature-complete Go client library for interacting with Steam Web APIs, Steam Community Market, Steam Trade Offers, and Steam Guard 2FA Authenticator.

Designed for production reliability, full concurrency safety (`sync.RWMutex`), and complete `context.Context` cancellation/timeout support.

---

## Features

- **Authentication & Web Session Management**:
  - Full support for modern Steam Web Auth API (RSA key retrieval, encrypted credentials, 2FA code verification, JWT tokens).
  - Mobile authenticator `.maFile` import & fast login via `RefreshToken`.
  - Automated session cookie synchronization across `steamcommunity.com` and `store.steampowered.com`.
  - Export and import session cookies in JSON format.
  - Custom HTTP Proxy support (`http/https/socks5`).

- **Steam Community Market**:
  - Fetch item prices (`FetchMarketPrice`) and historical price overview (`GetMarketPriceHistory`).
  - Create and cancel sell orders (`CreateSellOrder`, `CancelSellOrder`).
  - Create automatic buy orders with automated 2FA confirmation handling (`CreateBuyOrder`, `CancelBuyOrder`).
  - Retrieve active market listings and buy orders (`GetMyMarketListings`, `GetMyBuyOrders`).

- **Steam Trade Offers**:
  - Fetch sent and received trade offers (`GetTradeOffers`, `GetTradeOffer`).
  - Send new trade offers using item lists and trade URL tokens (`SendTradeOffer`).
  - Accept, decline, or cancel trade offers (`AcceptTradeOffer`, `DeclineTradeOffer`, `CancelTradeOffer`).
  - Retrieve user inventory for CS2 (`730`), Dota 2 (`570`), TF2 (`440`), and Steam items (`753`).

- **Steam Guard 2FA & Confirmations**:
  - Generate 2FA TOTP login codes (`Generate2FACode`).
  - List and approve mobile confirmations for trade offers and market listings (`GetConfirmations`, `SendConfirmationAction`).

---

## Installation

```bash
go get github.com/romanvolkov650/go-steam
```

---

## Quick Start

### Initialize Client

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/romanvolkov650/go-steam"
)

func main() {
	client, err := steam.NewClient(steam.ClientConfig{
		Username:       "your_steam_username",
		Password:       "your_steam_password",
		SharedSecret:   "your_shared_secret",   // from .maFile
		IdentitySecret: "your_identity_secret", // from .maFile
		ProxyURL:       "http://proxy:8080",    // optional
	})
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Login using Web Auth API
	if err := client.LoginWithContext(ctx); err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	fmt.Println("Successfully logged into Steam!")
}
```

### Fast Login via maFile (`RefreshToken`)

```go
maFile, err := steam.LoadMAFile("path/to/account.maFile")
if err != nil {
	log.Fatal(err)
}

client, err := steam.NewClientFromMAFile(maFile)
if err != nil {
	log.Fatal(err)
}

// Log in instantly using stored RefreshToken
if err := client.LoginWithRefreshToken(); err != nil {
	log.Fatalf("Refresh token login failed: %v", err)
}
```

### Save and Load Session Cookies

```go
// Export session cookies to file
if err := client.SaveCookiesToFile("cookies.json"); err != nil {
	log.Fatal(err)
}

// Load session cookies on next app startup
if err := client.LoadCookiesFromFile("cookies.json"); err != nil {
	log.Fatal(err)
}
```

### Steam Community Market Usage

```go
// Fetch price overview for CS2 item (AppID 730)
price, err := client.FetchMarketPrice(730, "AK-47 | Redline (Field-Tested)")
if err == nil {
	fmt.Printf("Lowest Price: %s, Volume: %s\n", price.LowestPrice, price.Volume)
}

// Create sell order (price in cents/kopecks, e.g., 500 = $5.00 / 5.00 RUB)
sellResp, err := client.CreateSellOrder("asset_id_here", 730, "2", 500)
if err == nil {
	fmt.Printf("Listed item! Listing ID: %s\n", sellResp.ListingID)
}

// Cancel sell order
err = client.CancelSellOrder("listing_id_here")
```

### Trade Offers Usage

```go
// Parse partner trade URL
partnerSteamID, tradeToken, err := steam.ParseTradeURL("https://steamcommunity.com/tradeoffer/new/?partner=12345678&token=abcdefgh")

itemsToGive := []steam.TradeItem{
	{AppID: 730, ContextID: "2", AssetID: "asset_id_1"},
}
itemsToReceive := []steam.TradeItem{}

offerID, requires2FA, err := client.SendTradeOffer(partnerSteamID, itemsToGive, itemsToReceive, "Trade offer message", tradeToken)
if err != nil {
	log.Fatalf("Failed to send trade offer: %v", err)
}

fmt.Printf("Trade offer sent! Offer ID: %s, Requires 2FA: %v\n", offerID, requires2FA)

// Accept trade offer
err = client.AcceptTradeOffer(offerID)
```

### 2FA Authenticator & Confirmations

```go
// Generate current 5-character Steam Guard TOTP code
code, err := client.Generate2FACode()
fmt.Printf("Current 2FA Code: %s\n", code)

// Get pending mobile confirmations
confs, err := client.GetConfirmations()
for _, conf := range confs {
	fmt.Printf("Confirmation: %s - %s\n", conf.ID, conf.Title)
	// Accept confirmation
	client.SendConfirmationAction(conf, "allow")
}
```

---

## Interface Architecture

`go-steam` exposes flexible service interfaces ([`steam.SteamClient`](file:///Users/romanvolkov/go-steam/interfaces.go)):

- `steam.MarketService`
- `steam.TradeOfferService`
- `steam.AuthenticatorService`
- `steam.AccountService`

This makes mocking and unit testing simple in application services consuming `go-steam`.

---

## License

MIT License. Free for personal and commercial use.
