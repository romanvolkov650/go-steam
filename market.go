package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// MarketPriceOverview contains price overview data for an item on the Steam Community Market.
type MarketPriceOverview struct {
	Success     bool   `json:"success"`
	LowestPrice string `json:"lowest_price"`
	Volume      string `json:"volume"`
	MedianPrice string `json:"median_price"`
}

// MarketPriceHistoryPoint represents a single historical price point on the Market.
type MarketPriceHistoryPoint struct {
	Timestamp int64   `json:"timestamp"`
	Price     float64 `json:"price"`
	Volume    int     `json:"volume"`
}

// MarketListing represents an active sell listing on the Market.
type MarketListing struct {
	ListingID   string `json:"listing_id"`
	AppID       int    `json:"appid"`
	ContextID   string `json:"contextid"`
	AssetID     string `json:"assetid"`
	Name        string `json:"name"`
	Price       string `json:"price"`
	TimeCreated int64  `json:"time_created"`
	IconURL     string `json:"icon_url,omitempty"`
}



// CreateSellOrderResponse represents the response when creating a sell listing.
type CreateSellOrderResponse struct {
	Success              bool         `json:"success"`
	RequiresConfirmation FlexibleBool `json:"requires_confirmation"`
	ListingID            string       `json:"listingid"`
	Error                string       `json:"message,omitempty"`
}

// FetchMarketPrice fetches current market price overview for an item.
func (c *Client) FetchMarketPrice(appID int, marketHashName string, currency ...int) (*MarketPriceOverview, error) {
	return c.FetchMarketPriceWithContext(context.Background(), appID, marketHashName, currency...)
}

// FetchMarketPriceWithContext fetches current market price overview for an item with context support.
func (c *Client) FetchMarketPriceWithContext(ctx context.Context, appID int, marketHashName string, currency ...int) (*MarketPriceOverview, error) {
	c.mu.RLock()
	walletCurrency := c.WalletCurrency
	c.mu.RUnlock()

	if walletCurrency <= 0 && (len(currency) == 0 || currency[0] <= 0) {
		_, _ = c.fetchWalletBalanceWithContext(ctx)
		c.mu.RLock()
		walletCurrency = c.WalletCurrency
		c.mu.RUnlock()
	}

	currencyID := walletCurrency
	if len(currency) > 0 && currency[0] > 0 {
		currencyID = currency[0]
	}
	if currencyID <= 0 {
		currencyID = 1 // Default USD
	}

	reqURL := fmt.Sprintf("https://steamcommunity.com/market/priceoverview/?country=US&currency=%d&appid=%d&market_hash_name=%s",
		currencyID, appID, url.QueryEscape(marketHashName))

	req, err := c.newRequestWithContext(ctx, "GET", reqURL, nil, fmt.Sprintf("https://steamcommunity.com/market/listings/%d/%s", appID, url.QueryEscape(marketHashName)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("market price request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &SteamAPIError{StatusCode: resp.StatusCode, Message: string(body)}
	}

	var overview MarketPriceOverview
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		return nil, fmt.Errorf("failed to decode price overview response: %w", err)
	}

	return &overview, nil
}

// GetMarketPriceHistory fetches historical price points for an item on the Steam Market.
func (c *Client) GetMarketPriceHistory(appID int, marketHashName string) ([]MarketPriceHistoryPoint, error) {
	return c.GetMarketPriceHistoryWithContext(context.Background(), appID, marketHashName)
}

// GetMarketPriceHistoryWithContext fetches historical price points for an item on the Steam Market with context support.
func (c *Client) GetMarketPriceHistoryWithContext(ctx context.Context, appID int, marketHashName string) ([]MarketPriceHistoryPoint, error) {
	reqURL := fmt.Sprintf("https://steamcommunity.com/market/pricehistory/?appid=%d&market_hash_name=%s",
		appID, url.QueryEscape(marketHashName))

	req, err := c.newRequestWithContext(ctx, "GET", reqURL, nil, fmt.Sprintf("https://steamcommunity.com/market/listings/%d/%s", appID, url.QueryEscape(marketHashName)))
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &SteamAPIError{StatusCode: resp.StatusCode, Message: string(body)}
	}

	var historyResp struct {
		Success bool            `json:"success"`
		Prices  [][]interface{} `json:"prices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&historyResp); err != nil {
		return nil, err
	}

	var points []MarketPriceHistoryPoint
	for _, p := range historyResp.Prices {
		if len(p) >= 3 {
			priceVal, _ := strconv.ParseFloat(fmt.Sprintf("%v", p[1]), 64)
			volumeVal, _ := strconv.Atoi(fmt.Sprintf("%v", p[2]))
			points = append(points, MarketPriceHistoryPoint{
				Price:  priceVal,
				Volume: volumeVal,
			})
		}
	}

	return points, nil
}

// CreateSellOrder lists an item from inventory for sale on the Steam Community Market.
func (c *Client) CreateSellOrder(assetID string, appID int, contextID string, priceInCents int, amount ...int) (*CreateSellOrderResponse, error) {
	return c.CreateSellOrderWithContext(context.Background(), assetID, appID, contextID, priceInCents, amount...)
}

// CreateSellOrderWithContext lists an item from inventory for sale on the Steam Community Market with context support.
func (c *Client) CreateSellOrderWithContext(ctx context.Context, assetID string, appID int, contextID string, priceInCents int, amount ...int) (*CreateSellOrderResponse, error) {
	sellURL := "https://steamcommunity.com/market/sellitem/"

	c.mu.RLock()
	sessionID := c.SessionID
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	amt := 1
	if len(amount) > 0 && amount[0] > 0 {
		amt = amount[0]
	}

	formData := url.Values{}
	formData.Set("sessionid", sessionID)
	formData.Set("appid", strconv.Itoa(appID))
	formData.Set("contextid", contextID)
	formData.Set("assetid", assetID)
	formData.Set("amount", strconv.Itoa(amt))
	formData.Set("price", strconv.Itoa(priceInCents))

	req, err := c.newAjaxPostRequestWithContext(ctx, sellURL, formData, fmt.Sprintf("https://steamcommunity.com/profiles/%s/inventory", steamID))
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute sell order request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &SteamAPIError{StatusCode: resp.StatusCode, Message: string(bodyBytes)}
	}

	var sellResp CreateSellOrderResponse
	if err := json.Unmarshal(bodyBytes, &sellResp); err != nil {
		return nil, fmt.Errorf("failed to parse create sell order response: %w", err)
	}

	if sellResp.Error != "" {
		return &sellResp, fmt.Errorf("steam create sell order error: %s", sellResp.Error)
	}

	return &sellResp, nil
}

// CreateBuyOrderResponse represents the response when creating a buy order.
type CreateBuyOrderResponse struct {
	Success              bool         `json:"success"`
	BuyOrderID           string       `json:"buy_orderid"`
	RequiresConfirmation FlexibleBool `json:"requires_confirmation"`
	NeedConfirmation     FlexibleBool `json:"need_confirmation"`
	Confirmation         *struct {
		ConfirmationID string `json:"confirmation_id"`
	} `json:"confirmation,omitempty"`
	Message string `json:"message,omitempty"`
}

// CreateBuyOrder places an automatic buy order for an item on the Steam Market.
func (c *Client) CreateBuyOrder(appID int, marketHashName string, priceInCents int, quantity int, currency ...int) (*CreateBuyOrderResponse, error) {
	return c.CreateBuyOrderWithContext(context.Background(), appID, marketHashName, priceInCents, quantity, currency...)
}

// CreateBuyOrderWithContext places an automatic buy order for an item on the Steam Market with context support.
func (c *Client) CreateBuyOrderWithContext(ctx context.Context, appID int, marketHashName string, priceInCents int, quantity int, currency ...int) (*CreateBuyOrderResponse, error) {
	return c.createBuyOrderInternalWithContext(ctx, appID, marketHashName, priceInCents, quantity, "", currency...)
}

func (c *Client) createBuyOrderInternal(appID int, marketHashName string, priceInCents int, quantity int, confirmationID string, currency ...int) (*CreateBuyOrderResponse, error) {
	return c.createBuyOrderInternalWithContext(context.Background(), appID, marketHashName, priceInCents, quantity, confirmationID, currency...)
}

func (c *Client) createBuyOrderInternalWithContext(ctx context.Context, appID int, marketHashName string, priceInCents int, quantity int, confirmationID string, currency ...int) (*CreateBuyOrderResponse, error) {
	buyURL := "https://steamcommunity.com/market/createbuyorder/"

	c.mu.RLock()
	walletCurrency := c.WalletCurrency
	sessionID := c.SessionID
	c.mu.RUnlock()

	currencyID := walletCurrency
	if len(currency) > 0 && currency[0] > 0 {
		currencyID = currency[0]
	}
	if currencyID <= 0 {
		currencyID = 5 // Default RUB (5) / USD (1)
	}

	if quantity <= 0 {
		quantity = 1
	}

	totalPrice := priceInCents * quantity

	formData := url.Values{}
	formData.Set("sessionid", sessionID)
	formData.Set("currency", strconv.Itoa(currencyID))
	formData.Set("appid", strconv.Itoa(appID))
	formData.Set("market_hash_name", marketHashName)
	formData.Set("price_total", strconv.Itoa(totalPrice))
	formData.Set("tradefee_tax", "0")
	formData.Set("quantity", strconv.Itoa(quantity))
	formData.Set("first_name", "")
	formData.Set("last_name", "")
	formData.Set("billing_address", "")
	formData.Set("billing_address_two", "")
	formData.Set("billing_country", "UA")
	formData.Set("billing_city", "")
	formData.Set("billing_state", "")
	formData.Set("billing_postal_code", "")
	if confirmationID != "" {
		formData.Set("confirmation", confirmationID)
	}
	formData.Set("save_my_address", "1")

	req, err := c.newAjaxPostRequestWithContext(ctx, buyURL, formData, fmt.Sprintf("https://steamcommunity.com/market/listings/%d/%s", appID, url.QueryEscape(marketHashName)))
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute create buy order request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rawResp struct {
		Success          interface{} `json:"success"`
		BuyOrderID       interface{} `json:"buy_orderid"`
		NeedConfirmation FlexibleBool `json:"need_confirmation"`
		Confirmation     *struct {
			ConfirmationID string `json:"confirmation_id"`
		} `json:"confirmation,omitempty"`
		Message string `json:"message,omitempty"`
	}

	// Unmarshal JSON response (Steam uses HTTP 200 or 406 for confirmation requests)
	if err := json.Unmarshal(bodyBytes, &rawResp); err == nil {
		buyResp := &CreateBuyOrderResponse{
			NeedConfirmation: rawResp.NeedConfirmation,
			Confirmation:     rawResp.Confirmation,
			Message:          rawResp.Message,
		}

		if rawResp.Success != nil {
			switch v := rawResp.Success.(type) {
			case bool:
				buyResp.Success = v
			case float64:
				buyResp.Success = (v == 1 || v == 22)
			}
		}

		if rawResp.BuyOrderID != nil {
			buyResp.BuyOrderID = fmt.Sprintf("%v", rawResp.BuyOrderID)
		}

		if (rawResp.NeedConfirmation || rawResp.Confirmation != nil) && confirmationID == "" {
			buyResp.RequiresConfirmation = true
			if rawResp.Confirmation != nil && rawResp.Confirmation.ConfirmationID != "" {
				confID := rawResp.Confirmation.ConfirmationID
				buyResp.BuyOrderID = confID

				// Step 2: Approve 2FA mobile confirmation if required
				_ = c.AcceptMarketListingConfirmationWithContext(ctx, confID)

				// Step 3: Send second POST request to createbuyorder with confirmation parameter
				step2Resp, step2Err := c.createBuyOrderInternalWithContext(ctx, appID, marketHashName, priceInCents, quantity, confID, currency...)
				if step2Err == nil && step2Resp != nil && step2Resp.BuyOrderID != "" {
					return step2Resp, nil
				}
			}
		}

		if buyResp.Message != "" && !bool(buyResp.RequiresConfirmation) {
			return buyResp, fmt.Errorf("steam create buy order error: %s", buyResp.Message)
		}

		return buyResp, nil
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotAcceptable {
		return nil, fmt.Errorf("create buy order returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil, fmt.Errorf("failed to parse buy order response: %s", string(bodyBytes))
}

// CancelSellOrder removes an active sell listing from the Market.
func (c *Client) CancelSellOrder(listingID string) error {
	return c.CancelSellOrderWithContext(context.Background(), listingID)
}

// CancelSellOrderWithContext removes an active sell listing from the Market with context support.
func (c *Client) CancelSellOrderWithContext(ctx context.Context, listingID string) error {
	cancelURL := fmt.Sprintf("https://steamcommunity.com/market/removelisting/%s", listingID)

	c.mu.RLock()
	sessionID := c.SessionID
	c.mu.RUnlock()

	formData := url.Values{}
	formData.Set("sessionid", sessionID)

	req, err := c.newAjaxPostRequestWithContext(ctx, cancelURL, formData, "https://steamcommunity.com/market/")
	if err != nil {
		return err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute cancel sell order request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read cancel sell order response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return &SteamAPIError{StatusCode: resp.StatusCode, Message: string(bodyBytes)}
	}

	var cancelResp struct {
		Success FlexibleBool `json:"success"`
		Message string       `json:"message,omitempty"`
	}
	if err := json.Unmarshal(bodyBytes, &cancelResp); err == nil && cancelResp.Message != "" {
		return fmt.Errorf("steam cancel sell order error: %s", cancelResp.Message)
	}

	return nil
}

// CancelBuyOrder cancels an active buy order on the Market.
func (c *Client) CancelBuyOrder(buyOrderID string) error {
	return c.CancelBuyOrderWithContext(context.Background(), buyOrderID)
}

// CancelBuyOrderWithContext cancels an active buy order on the Market with context support.
func (c *Client) CancelBuyOrderWithContext(ctx context.Context, buyOrderID string) error {
	cancelURL := "https://steamcommunity.com/market/cancelbuyorder/"

	c.mu.RLock()
	sessionID := c.SessionID
	c.mu.RUnlock()

	formData := url.Values{}
	formData.Set("sessionid", sessionID)
	formData.Set("buy_orderid", buyOrderID)
	formData.Set("buy_order_id", buyOrderID)

	req, err := c.newAjaxPostRequestWithContext(ctx, cancelURL, formData, "https://steamcommunity.com/market/")
	if err != nil {
		return err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute cancel buy order request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read cancel buy order response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return &SteamAPIError{StatusCode: resp.StatusCode, Message: string(bodyBytes)}
	}

	var cancelResp struct {
		Success FlexibleBool `json:"success"`
		Message string       `json:"message,omitempty"`
	}
	if err := json.Unmarshal(bodyBytes, &cancelResp); err == nil {
		if cancelResp.Message != "" {
			if strings.Contains(cancelResp.Message, "already have an active buy order") {
				return ErrBuyOrderAlreadyExists
			}
			return fmt.Errorf("steam cancel buy order error: %s", cancelResp.Message)
		}
	}

	return nil
}

type myListingsRenderResponse struct {
	Success         bool   `json:"success"`
	ResultsHTML     string `json:"results_html"`
	MyBuyOrdersHTML string `json:"my_buy_orders_html"`
	BuyOrdersHTML   string `json:"buy_orders_html"`
}

func (c *Client) fetchMyListingsRenderWithContext(ctx context.Context) (*myListingsRenderResponse, error) {
	renderURL := "https://steamcommunity.com/market/mylistings/render/?query=&start=0&count=100"

	req, err := c.newRequestWithContext(ctx, "GET", renderURL, nil, "https://steamcommunity.com/market/")
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var listingsResp myListingsRenderResponse
	if err := json.Unmarshal(bodyBytes, &listingsResp); err != nil {
		return nil, err
	}

	return &listingsResp, nil
}

// GetMyMarketListings fetches user's active sell listings from the Steam Market.
func (c *Client) GetMyMarketListings() ([]*MarketListing, error) {
	return c.GetMyMarketListingsWithContext(context.Background())
}

// GetMyMarketListingsWithContext fetches user's active sell listings from the Steam Market with context support.
func (c *Client) GetMyMarketListingsWithContext(ctx context.Context) ([]*MarketListing, error) {
	listingsResp, err := c.fetchMyListingsRenderWithContext(ctx)
	if err != nil {
		return nil, err
	}

	var listings []*MarketListing
	reListing := regexp.MustCompile(`(?:mylisting_|removelisting/)(\d+)`)
	matches := reListing.FindAllStringSubmatch(listingsResp.ResultsHTML, -1)

	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 {
			id := m[1]
			if seen[id] {
				continue
			}
			seen[id] = true
			listings = append(listings, &MarketListing{
				ListingID: id,
			})
		}
	}

	return listings, nil
}

// MarketBuyOrder represents an active buy order on the Market.
type MarketBuyOrder struct {
	BuyOrderID string `json:"buy_order_id"`
}

// GetMyBuyOrders fetches user's active buy orders from the Steam Market.
func (c *Client) GetMyBuyOrders() ([]*MarketBuyOrder, error) {
	return c.GetMyBuyOrdersWithContext(context.Background())
}

// GetMyBuyOrdersWithContext fetches user's active buy orders from the Steam Market with context support.
func (c *Client) GetMyBuyOrdersWithContext(ctx context.Context) ([]*MarketBuyOrder, error) {
	listingsResp, err := c.fetchMyListingsRenderWithContext(ctx)
	if err != nil {
		return nil, err
	}

	var orders []*MarketBuyOrder
	reBuyOrder := regexp.MustCompile(`(?:mybuyorder_|mbuyorder_|CancelMarketBuyOrder\s*\(\s*['"]?)(\d+)`)

	combinedHTML := listingsResp.MyBuyOrdersHTML + "\n" + listingsResp.BuyOrdersHTML + "\n" + listingsResp.ResultsHTML
	matches := reBuyOrder.FindAllStringSubmatch(combinedHTML, -1)

	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 {
			id := m[1]
			if seen[id] {
				continue
			}
			seen[id] = true
			orders = append(orders, &MarketBuyOrder{
				BuyOrderID: id,
			})
		}
	}

	return orders, nil
}

// AcceptMarketListingConfirmation confirms a market sell listing using Steam Guard mobile confirmations.
func (c *Client) AcceptMarketListingConfirmation(listingID string) error {
	return c.approveConfirmationForID(listingID)
}

// AcceptMarketListingConfirmationWithContext confirms a market sell listing using Steam Guard mobile confirmations with context support.
func (c *Client) AcceptMarketListingConfirmationWithContext(ctx context.Context, listingID string) error {
	return c.approveConfirmationForIDWithContext(ctx, listingID)
}
