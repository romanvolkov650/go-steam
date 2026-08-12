package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TradeOfferState represents the status of a Steam trade offer.
type TradeOfferState int

const (
	TradeOfferStateInvalid                  TradeOfferState = 1
	TradeOfferStateActive                   TradeOfferState = 2
	TradeOfferStateAccepted                 TradeOfferState = 3
	TradeOfferStateCountered                TradeOfferState = 4
	TradeOfferStateExpired                  TradeOfferState = 5
	TradeOfferStateCanceled                 TradeOfferState = 6
	TradeOfferStateDeclined                 TradeOfferState = 7
	TradeOfferStateInvalidItems             TradeOfferState = 8
	TradeOfferStateCreatedNeedsConfirmation TradeOfferState = 9
	TradeOfferStateCanceledBySecondFactor   TradeOfferState = 10
	TradeOfferStateInEscrow                 TradeOfferState = 11
)

// TradeItem represents a single item inside a trade offer.
type TradeItem struct {
	AssetID    string `json:"assetid"`
	AppID      int    `json:"appid"`
	ContextID  string `json:"contextid"`
	Amount     string `json:"amount,omitempty"`
	ClassID    string `json:"classid,omitempty"`
	InstanceID string `json:"instanceid,omitempty"`
	Name       string `json:"name,omitempty"`
	IconURL    string `json:"icon_url,omitempty"`
	Tradable   bool   `json:"tradable"`
}

// TradeOffer represents a Steam trade offer.
type TradeOffer struct {
	TradeOfferID   string          `json:"tradeofferid"`
	PartnerSteamID string          `json:"partner_steam_id"`
	Message        string          `json:"message"`
	State          TradeOfferState `json:"trade_offer_state"`
	IsSent         bool            `json:"is_sent"`
	IsOurOffer     bool            `json:"is_our_offer"`
	ItemsToGive    []TradeItem     `json:"items_to_give"`
	ItemsToReceive []TradeItem     `json:"items_to_receive"`
	TimeCreated    int64           `json:"time_created"`
	TimeUpdated    int64           `json:"time_updated"`
	TimeExpiration int64           `json:"time_expiration"`
	EscrowEndDate  int64           `json:"escrow_end_date,omitempty"`
}

// GetTradeOffersOptions options for fetching trade offers list.
type GetTradeOffersOptions struct {
	GetSent         bool   `json:"get_sent"`
	GetReceived     bool   `json:"get_received"`
	GetDescriptions bool   `json:"get_descriptions"`
	CutoffTime      int64  `json:"cutoff_time,omitempty"`
	AccessToken     string `json:"access_token,omitempty"`
	APIKey          string `json:"api_key,omitempty"`
}

// Structures for parsing IEconService responses
type econServiceOffersResponse struct {
	Response struct {
		TradeOffersSent     []econTradeOffer      `json:"trade_offers_sent"`
		TradeOffersReceived []econTradeOffer      `json:"trade_offers_received"`
		Descriptions        []econItemDescription `json:"descriptions"`
	} `json:"response"`
}

type econSingleOfferResponse struct {
	Response struct {
		Offer        econTradeOffer        `json:"offer"`
		Descriptions []econItemDescription `json:"descriptions"`
	} `json:"response"`
}

type econTradeOffer struct {
	TradeOfferID      string          `json:"tradeofferid"`
	AccountIDOther    uint32          `json:"accountid_other"`
	Message           string          `json:"message"`
	ExpirationTime    int64           `json:"expiration_time"`
	TradeOfferState   TradeOfferState `json:"trade_offer_state"`
	ItemsToGive       []econAsset     `json:"items_to_give"`
	ItemsToReceive    []econAsset     `json:"items_to_receive"`
	IsOurOffer        bool            `json:"is_our_offer"`
	TimeCreated       int64           `json:"time_created"`
	TimeUpdated       int64           `json:"time_updated"`
	FromRealTimeTrade bool            `json:"from_real_time_trade"`
	EscrowEndDate     int64           `json:"escrow_end_date"`
}

type econAsset struct {
	AppID      int    `json:"appid"`
	ContextID  string `json:"contextid"`
	AssetID    string `json:"assetid"`
	Amount     string `json:"amount"`
	ClassID    string `json:"classid"`
	InstanceID string `json:"instanceid"`
}

func (a *econAsset) UnmarshalJSON(data []byte) error {
	type Alias econAsset
	aux := &struct {
		AppID interface{} `json:"appid"`
		*Alias
	}{
		Alias: (*Alias)(a),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	switch v := aux.AppID.(type) {
	case float64:
		a.AppID = int(v)
	case string:
		a.AppID, _ = strconv.Atoi(v)
	}
	return nil
}

type econItemDescription struct {
	AppID      int          `json:"appid"`
	ClassID    string       `json:"classid"`
	InstanceID string       `json:"instanceid"`
	MarketName string       `json:"market_name"`
	Name       string       `json:"name"`
	IconURL    string       `json:"icon_url"`
	Tradable   FlexibleBool `json:"tradable"`
}

func (d *econItemDescription) UnmarshalJSON(data []byte) error {
	type Alias econItemDescription
	aux := &struct {
		AppID interface{} `json:"appid"`
		*Alias
	}{
		Alias: (*Alias)(d),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	switch v := aux.AppID.(type) {
	case float64:
		d.AppID = int(v)
	case string:
		d.AppID, _ = strconv.Atoi(v)
	}
	return nil
}

// GetTradeOffers fetches sent and/or received trade offers.
func (c *Client) GetTradeOffers(opts GetTradeOffersOptions) ([]*TradeOffer, error) {
	return c.GetTradeOffersWithContext(context.Background(), opts)
}

// GetTradeOffersWithContext fetches sent and/or received trade offers with context support.
func (c *Client) GetTradeOffersWithContext(ctx context.Context, opts GetTradeOffersOptions) ([]*TradeOffer, error) {
	c.mu.RLock()
	accessToken := c.Config.AccessToken
	c.mu.RUnlock()

	token := opts.AccessToken
	if token == "" {
		token = accessToken
	}

	apiKey := opts.APIKey

	if token != "" || apiKey != "" {
		offers, err := c.getTradeOffersViaAPIWithContext(ctx, opts, token, apiKey)
		if err == nil {
			return offers, nil
		}
	}

	return c.getTradeOffersViaWebSessionWithContext(ctx, opts)
}

func (c *Client) getTradeOffersViaAPIWithContext(ctx context.Context, opts GetTradeOffersOptions, token, apiKey string) ([]*TradeOffer, error) {
	reqURL := "https://api.steampowered.com/IEconService/GetTradeOffers/v1/?"
	values := url.Values{}

	if token != "" {
		values.Set("access_token", token)
	} else if apiKey != "" {
		values.Set("key", apiKey)
	}

	if opts.GetSent {
		values.Set("get_sent_offers", "1")
	}
	if opts.GetReceived {
		values.Set("get_received_offers", "1")
	}
	if opts.GetDescriptions {
		values.Set("get_descriptions", "1")
	}
	if opts.CutoffTime > 0 {
		values.Set("time_historical_cutoff", strconv.FormatInt(opts.CutoffTime, 10))
	}
	values.Set("language", "english")

	req, err := c.newRequestWithContext(ctx, "GET", reqURL+values.Encode(), nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	bodyBytes, resp, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute IEconService request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &SteamAPIError{StatusCode: resp.StatusCode, Message: string(bodyBytes)}
	}

	var econResp econServiceOffersResponse
	if err := json.Unmarshal(bodyBytes, &econResp); err != nil {
		return nil, fmt.Errorf("failed to decode IEconService response: %w", err)
	}

	descMap := make(map[string]econItemDescription)
	for _, d := range econResp.Response.Descriptions {
		key := fmt.Sprintf("%d_%s_%s", d.AppID, d.ClassID, d.InstanceID)
		descMap[key] = d
	}

	var offers []*TradeOffer

	for _, o := range econResp.Response.TradeOffersSent {
		offers = append(offers, parseEconOffer(o, true, descMap))
	}
	for _, o := range econResp.Response.TradeOffersReceived {
		offers = append(offers, parseEconOffer(o, false, descMap))
	}

	return offers, nil
}

func applyItemDescription(item *TradeItem, d econItemDescription) {
	item.Name = d.MarketName
	if item.Name == "" {
		item.Name = d.Name
	}
	item.IconURL = d.IconURL
	item.Tradable = bool(d.Tradable)
}

func convertEconAssetsToTradeItems(assets []econAsset, descMap map[string]econItemDescription) []TradeItem {
	items := make([]TradeItem, 0, len(assets))
	for _, a := range assets {
		item := TradeItem{
			AssetID:    a.AssetID,
			AppID:      a.AppID,
			ContextID:  a.ContextID,
			Amount:     a.Amount,
			ClassID:    a.ClassID,
			InstanceID: a.InstanceID,
		}
		key := fmt.Sprintf("%d_%s_%s", a.AppID, a.ClassID, a.InstanceID)
		if d, ok := descMap[key]; ok {
			applyItemDescription(&item, d)
		}
		items = append(items, item)
	}
	return items
}

func parseEconOffer(o econTradeOffer, isSent bool, descMap map[string]econItemDescription) *TradeOffer {
	partnerSteamID := accountIDToSteamID64(o.AccountIDOther)

	return &TradeOffer{
		TradeOfferID:   o.TradeOfferID,
		PartnerSteamID: partnerSteamID,
		Message:        o.Message,
		State:          o.TradeOfferState,
		IsSent:         isSent,
		IsOurOffer:     o.IsOurOffer,
		ItemsToGive:    convertEconAssetsToTradeItems(o.ItemsToGive, descMap),
		ItemsToReceive: convertEconAssetsToTradeItems(o.ItemsToReceive, descMap),
		TimeCreated:    o.TimeCreated,
		TimeUpdated:    o.TimeUpdated,
		TimeExpiration: o.ExpirationTime,
		EscrowEndDate:  o.EscrowEndDate,
	}
}

func (c *Client) getTradeOffersViaWebSession(opts GetTradeOffersOptions) ([]*TradeOffer, error) {
	return c.getTradeOffersViaWebSessionWithContext(context.Background(), opts)
}

func (c *Client) getTradeOffersViaWebSessionWithContext(ctx context.Context, opts GetTradeOffersOptions) ([]*TradeOffer, error) {
	var offers []*TradeOffer

	if opts.GetReceived || (!opts.GetSent && !opts.GetReceived) {
		recOffers, err := c.fetchWebTradeOffersPageWithContext(ctx, "https://steamcommunity.com/my/tradeoffers/", false)
		if err == nil {
			offers = append(offers, recOffers...)
		}
	}

	if opts.GetSent || (!opts.GetSent && !opts.GetReceived) {
		sentOffers, err := c.fetchWebTradeOffersPageWithContext(ctx, "https://steamcommunity.com/my/tradeoffers/sent/", true)
		if err == nil {
			offers = append(offers, sentOffers...)
		}
	}

	return offers, nil
}

func (c *Client) fetchWebTradeOffersPage(pageURL string, isSent bool) ([]*TradeOffer, error) {
	return c.fetchWebTradeOffersPageWithContext(context.Background(), pageURL, isSent)
}

func (c *Client) fetchWebTradeOffersPageWithContext(ctx context.Context, pageURL string, isSent bool) ([]*TradeOffer, error) {
	req, err := c.newRequestWithContext(ctx, "GET", pageURL, nil, "")
	if err != nil {
		return nil, err
	}

	bodyBytes, _, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return nil, err
	}

	return parseTradeOffersHTML(string(bodyBytes), isSent), nil
}

func parseTradeOffersHTML(bodyStr string, isSent bool) []*TradeOffer {
	// Split by trade offer block: id="tradeofferid_XXXXX"
	rePartner := regexp.MustCompile(`(?:profiles/|ReportTradeScam\(\s*['"]?)(\d{17})`)

	blocks := strings.Split(bodyStr, `id="tradeofferid_`)
	var offers []*TradeOffer
	seen := make(map[string]bool)

	for _, block := range blocks[1:] {
		// First characters before " or > are the offer ID
		idEnd := strings.IndexAny(block, `"' >`)
		if idEnd == -1 {
			continue
		}
		id := block[:idEnd]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true

		partnerID := ""
		if m := rePartner.FindStringSubmatch(block); len(m) > 1 {
			partnerID = m[1]
		}

		offer := &TradeOffer{
			TradeOfferID:   id,
			PartnerSteamID: partnerID,
			IsSent:         isSent,
			State:          TradeOfferStateActive,
		}
		offers = append(offers, offer)
	}

	return offers
}

// GetUserInventory fetches full list of inventory items for specified appID and contextID for the current client account.
func (c *Client) GetUserInventory(appID, contextID string) ([]TradeItem, error) {
	return c.GetUserInventoryWithContext(context.Background(), appID, contextID)
}

// GetUserInventoryWithContext fetches full list of inventory items for specified appID and contextID for the current client account with context support.
func (c *Client) GetUserInventoryWithContext(ctx context.Context, appID, contextID string) ([]TradeItem, error) {
	c.mu.RLock()
	steamID := c.Config.SteamID
	c.mu.RUnlock()
	return c.GetPartnerInventoryWithContext(ctx, steamID, appID, contextID)
}

// GetPartnerInventory fetches full list of inventory items for any specified SteamID64, appID, and contextID.
func (c *Client) GetPartnerInventory(steamID, appID, contextID string) ([]TradeItem, error) {
	return c.GetPartnerInventoryWithContext(context.Background(), steamID, appID, contextID)
}

// GetPartnerInventoryWithContext fetches full list of inventory items for any specified SteamID64, appID, and contextID with context support.
func (c *Client) GetPartnerInventoryWithContext(ctx context.Context, steamID, appID, contextID string) ([]TradeItem, error) {
	if steamID == "" {
		return nil, fmt.Errorf("steamID is required for inventory request")
	}

	reqURL := fmt.Sprintf("https://steamcommunity.com/inventory/%s/%s/%s?l=english&count=1000&preserve_bbcode=1&raw_asset_properties=1", steamID, appID, contextID)

	var lastErr error
	var bodyBytes []byte

	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1500 * time.Millisecond):
			}
		}

		req, err := c.newRequestWithContext(ctx, "GET", reqURL, nil, fmt.Sprintf("https://steamcommunity.com/profiles/%s/inventory", steamID))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "*/*")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")

		resp, err := c.doRequestWithRetry(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}

		b, readErr := readResponseBody(resp)

		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode == http.StatusOK {
			bodyBytes = b
			lastErr = nil
			break
		}

		lastErr = &SteamAPIError{StatusCode: resp.StatusCode, Message: string(b)}
	}

	if lastErr != nil {
		return nil, lastErr
	}

	var inventoryResp struct {
		Success    FlexibleBool `json:"success"`
		Error      string       `json:"error"`
		TotalCount int          `json:"total_inventory_count"`
		Assets     []struct {
			AppID      int    `json:"appid"`
			ContextID  string `json:"contextid"`
			AssetID    string `json:"assetid"`
			Amount     string `json:"amount"`
			ClassID    string `json:"classid"`
			InstanceID string `json:"instanceid"`
		} `json:"assets"`
		Descriptions []econItemDescription `json:"descriptions"`
	}

	if err := json.Unmarshal(bodyBytes, &inventoryResp); err != nil {
		snippet := string(bodyBytes)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("failed to parse Steam inventory JSON for %s: %w (response snippet: %s)", steamID, err, snippet)
	}

	if inventoryResp.Error != "" {
		return nil, fmt.Errorf("steam inventory error for %s: %s", steamID, inventoryResp.Error)
	}

	descMap := make(map[string]econItemDescription)
	for _, d := range inventoryResp.Descriptions {
		key := fmt.Sprintf("%s_%s", d.ClassID, d.InstanceID)
		descMap[key] = d
	}

	var items []TradeItem
	for _, a := range inventoryResp.Assets {
		amount := a.Amount
		if amount == "" {
			amount = "1"
		}
		item := TradeItem{
			AssetID:    a.AssetID,
			AppID:      a.AppID,
			ContextID:  a.ContextID,
			Amount:     amount,
			ClassID:    a.ClassID,
			InstanceID: a.InstanceID,
		}
		key := fmt.Sprintf("%s_%s", a.ClassID, a.InstanceID)
		if d, ok := descMap[key]; ok {
			applyItemDescription(&item, d)
		}
		items = append(items, item)
	}

	return items, nil
}

// GetTradeOffer fetches details for a specific trade offer ID.
func (c *Client) GetTradeOffer(tradeOfferID string, opts ...GetTradeOffersOptions) (*TradeOffer, error) {
	return c.GetTradeOfferWithContext(context.Background(), tradeOfferID, opts...)
}

// GetTradeOfferWithContext fetches details for a specific trade offer ID with context support.
func (c *Client) GetTradeOfferWithContext(ctx context.Context, tradeOfferID string, opts ...GetTradeOffersOptions) (*TradeOffer, error) {
	var opt GetTradeOffersOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	c.mu.RLock()
	accessToken := c.Config.AccessToken
	c.mu.RUnlock()

	token := opt.AccessToken
	if token == "" {
		token = accessToken
	}
	apiKey := opt.APIKey

	if token != "" || apiKey != "" {
		reqURL := "https://api.steampowered.com/IEconService/GetTradeOffer/v1/?"
		values := url.Values{}
		if token != "" {
			values.Set("access_token", token)
		} else {
			values.Set("key", apiKey)
		}
		values.Set("tradeofferid", tradeOfferID)
		values.Set("language", "english")

		req, err := c.newRequestWithContext(ctx, "GET", reqURL+values.Encode(), nil, "")
		if err == nil {
			bodyBytes, resp, err := c.doRequestAndRead(ctx, req)
			if err == nil && resp.StatusCode == http.StatusOK {
				var singleResp econSingleOfferResponse
				if err := json.Unmarshal(bodyBytes, &singleResp); err == nil && singleResp.Response.Offer.TradeOfferID != "" {
					descMap := make(map[string]econItemDescription)
					for _, d := range singleResp.Response.Descriptions {
						key := fmt.Sprintf("%d_%s_%s", d.AppID, d.ClassID, d.InstanceID)
						descMap[key] = d
					}
					isSent := singleResp.Response.Offer.IsOurOffer
					return parseEconOffer(singleResp.Response.Offer, isSent, descMap), nil
				}
			}
		}
	}

	// Fallback to Web session HTML scrape
	webURL := fmt.Sprintf("https://steamcommunity.com/tradeoffer/%s/", tradeOfferID)
	req, err := c.newRequestWithContext(ctx, "GET", webURL, nil, "")
	if err != nil {
		return nil, err
	}

	bodyBytes, _, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return nil, err
	}
	bodyStr := string(bodyBytes)

	if strings.Contains(bodyStr, "You have logged in from a new device. In order to protect the items") {
		return nil, ErrNewDeviceTradeCooldown
	}

	// Extract partner Steam ID (User.steamid / partner_steamid / g_ulTradePartnerSteamID)
	offer := &TradeOffer{
		TradeOfferID: tradeOfferID,
		State:        TradeOfferStateActive,
	}

	rePartner := regexp.MustCompile(`g_ulTradePartnerSteamID\s*=\s*['"]?(\d+)['"]?`)
	if m := rePartner.FindStringSubmatch(bodyStr); len(m) > 1 {
		offer.PartnerSteamID = m[1]
	} else {
		return nil, fmt.Errorf("trade offer is not active or partner ID not found")
	}

	return offer, nil
}

// AcceptTradeOffer accepts an incoming trade offer.
func (c *Client) AcceptTradeOffer(tradeOfferID string) error {
	return c.AcceptTradeOfferWithContext(context.Background(), tradeOfferID)
}

// AcceptTradeOfferWithContext accepts an incoming trade offer with context support.
func (c *Client) AcceptTradeOfferWithContext(ctx context.Context, tradeOfferID string) error {
	sessionID := c.GetSessionID()

	offer, err := c.GetTradeOfferWithContext(ctx, tradeOfferID)
	if err != nil || offer.PartnerSteamID == "" {
		return fmt.Errorf("failed to fetch trade partner ID: %v", err)
	}
	partnerID := offer.PartnerSteamID

	acceptURL := fmt.Sprintf("https://steamcommunity.com/tradeoffer/%s/accept", tradeOfferID)
	formData := url.Values{}
	formData.Set("sessionid", sessionID)
	formData.Set("serverid", "1")
	formData.Set("tradeofferid", tradeOfferID)
	formData.Set("partner", partnerID)
	formData.Set("captcha", "")

	req, err := c.newAjaxPostRequestWithContext(ctx, acceptURL, formData, fmt.Sprintf("https://steamcommunity.com/tradeoffer/%s/", tradeOfferID))
	if err != nil {
		return err
	}

	bodyBytes, resp, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute accept trade offer request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &SteamAPIError{StatusCode: resp.StatusCode, Message: string(bodyBytes)}
	}

	var acceptResp struct {
		TradeID                 interface{} `json:"tradeid"`
		NeedsMobileConfirmation bool        `json:"needs_mobile_confirmation"`
		NeedsEmailConfirmation  bool        `json:"needs_email_confirmation"`
		Error                   string      `json:"strError"`
	}

	if err := json.Unmarshal(bodyBytes, &acceptResp); err == nil && acceptResp.Error != "" {
		return fmt.Errorf("steam trade accept error: %s", acceptResp.Error)
	}

	if acceptResp.NeedsMobileConfirmation {
		if err := c.AcceptTradeOfferConfirmationWithContext(ctx, tradeOfferID); err != nil {
			return fmt.Errorf("trade accepted but mobile confirmation failed: %w", err)
		}
	}

	return nil
}

// DeclineTradeOffer declines an incoming trade offer.
func (c *Client) DeclineTradeOffer(tradeOfferID string) error {
	return c.DeclineTradeOfferWithContext(context.Background(), tradeOfferID)
}

// DeclineTradeOfferWithContext declines an incoming trade offer with context support.
func (c *Client) DeclineTradeOfferWithContext(ctx context.Context, tradeOfferID string) error {
	sessionID := c.GetSessionID()

	declineURL := fmt.Sprintf("https://steamcommunity.com/tradeoffer/%s/decline", tradeOfferID)
	formData := url.Values{}
	formData.Set("sessionid", sessionID)

	req, err := c.newAjaxPostRequestWithContext(ctx, declineURL, formData, fmt.Sprintf("https://steamcommunity.com/tradeoffer/%s/", tradeOfferID))
	if err != nil {
		return err
	}

	bodyBytes, resp, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute decline request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return &SteamAPIError{StatusCode: resp.StatusCode, Message: string(bodyBytes)}
	}

	return nil
}

// CancelTradeOffer cancels an outgoing (sent) trade offer.
func (c *Client) CancelTradeOffer(tradeOfferID string) error {
	return c.CancelTradeOfferWithContext(context.Background(), tradeOfferID)
}

// CancelTradeOfferWithContext cancels an outgoing (sent) trade offer with context support.
func (c *Client) CancelTradeOfferWithContext(ctx context.Context, tradeOfferID string) error {
	sessionID := c.GetSessionID()

	cancelURL := fmt.Sprintf("https://steamcommunity.com/tradeoffer/%s/cancel", tradeOfferID)
	formData := url.Values{}
	formData.Set("sessionid", sessionID)

	req, err := c.newAjaxPostRequestWithContext(ctx, cancelURL, formData, fmt.Sprintf("https://steamcommunity.com/tradeoffer/%s/sent/", tradeOfferID))
	if err != nil {
		return err
	}

	bodyBytes, resp, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute cancel request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return &SteamAPIError{StatusCode: resp.StatusCode, Message: string(bodyBytes)}
	}

	return nil
}

// SendTradeOffer creates and sends a trade offer to specified tradeURL.
func (c *Client) SendTradeOffer(tradeURL string, itemsToGive, itemsToReceive []TradeItem, message string) (string, bool, error) {
	return c.SendTradeOfferWithContext(context.Background(), tradeURL, itemsToGive, itemsToReceive, message)
}

// SendTradeOfferWithContext creates and sends a trade offer to specified tradeURL with context support.
func (c *Client) SendTradeOfferWithContext(ctx context.Context, tradeURL string, itemsToGive, itemsToReceive []TradeItem, message string) (string, bool, error) {
	sendURL := "https://steamcommunity.com/tradeoffer/new/send"

	sessionID := c.GetSessionID()

	partnerSteamID, tradeToken, err := ParseTradeURL(tradeURL)
	if err != nil {
		return "", false, fmt.Errorf("invalid trade URL '%s': %w", tradeURL, err)
	}

	type tradeAsset struct {
		AppID     string `json:"appid"`
		ContextID string `json:"contextid"`
		Amount    int    `json:"amount"`
		AssetID   string `json:"assetid"`
	}

	type sideItems struct {
		Assets   []tradeAsset  `json:"assets"`
		Currency []interface{} `json:"currency"`
		Ready    bool          `json:"ready"`
	}

	type jsonTradeOfferPayload struct {
		NewVersion    bool      `json:"newversion"`
		Version       int       `json:"version,omitempty"`
		SinglePartner bool      `json:"single_partner,omitempty"`
		Me            sideItems `json:"me"`
		Them          sideItems `json:"them"`
	}

	meAssets := make([]tradeAsset, 0, len(itemsToGive))
	for _, item := range itemsToGive {
		amount := 1
		if item.Amount != "" {
			amount, _ = strconv.Atoi(item.Amount)
			if amount <= 0 {
				amount = 1
			}
		}
		appIDStr := strconv.Itoa(item.AppID)
		contextIDStr := item.ContextID
		if contextIDStr == "" {
			contextIDStr = "2"
		}

		meAssets = append(meAssets, tradeAsset{
			AppID:     appIDStr,
			ContextID: contextIDStr,
			Amount:    amount,
			AssetID:   item.AssetID,
		})
	}

	themAssets := make([]tradeAsset, 0, len(itemsToReceive))
	for _, item := range itemsToReceive {
		amount := 1
		if item.Amount != "" {
			amount, _ = strconv.Atoi(item.Amount)
			if amount <= 0 {
				amount = 1
			}
		}
		appIDStr := strconv.Itoa(item.AppID)
		contextIDStr := item.ContextID
		if contextIDStr == "" {
			contextIDStr = "2"
		}

		themAssets = append(themAssets, tradeAsset{
			AppID:     appIDStr,
			ContextID: contextIDStr,
			Amount:    amount,
			AssetID:   item.AssetID,
		})
	}

	offerPayload := jsonTradeOfferPayload{
		NewVersion: true,
		Version:    2,
		Me: sideItems{
			Assets:   meAssets,
			Currency: make([]interface{}, 0),
			Ready:    false,
		},
		Them: sideItems{
			Assets:   themAssets,
			Currency: make([]interface{}, 0),
			Ready:    false,
		},
	}

	jsonOfferBytes, err := json.Marshal(offerPayload)
	if err != nil {
		return "", false, fmt.Errorf("failed to marshal json_tradeoffer: %w", err)
	}

	paramsPayload := make(map[string]interface{})
	if tradeToken != "" {
		paramsPayload["trade_offer_access_token"] = tradeToken
	}

	jsonParamsBytes, _ := json.Marshal(paramsPayload)

	formData := url.Values{}
	formData.Set("sessionid", sessionID)
	formData.Set("serverid", "1")
	formData.Set("partner", partnerSteamID)
	formData.Set("tradeoffermessage", message)
	formData.Set("json_tradeoffer", string(jsonOfferBytes))
	formData.Set("captcha", "")
	formData.Set("trade_offer_create_params", string(jsonParamsBytes))

	refererURL := "https://steamcommunity.com/tradeoffer/new/"
	if tradeToken != "" {
		accID := SteamID64ToAccountID(partnerSteamID)
		if accID > 0 {
			refererURL = fmt.Sprintf("https://steamcommunity.com/tradeoffer/new/?partner=%d&token=%s", accID, tradeToken)
		}
	}

	req, err := c.newAjaxPostRequestWithContext(ctx, sendURL, formData, refererURL)
	if err != nil {
		return "", false, err
	}

	bodyBytes, resp, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return "", false, fmt.Errorf("failed to send trade offer: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", false, &SteamAPIError{StatusCode: resp.StatusCode, Message: string(bodyBytes)}
	}

	var sendResp struct {
		TradeOfferID            string `json:"tradeofferid"`
		NeedsMobileConfirmation bool   `json:"needs_mobile_confirmation"`
		Error                   string `json:"strError"`
	}

	if err := json.Unmarshal(bodyBytes, &sendResp); err != nil {
		return "", false, fmt.Errorf("failed to parse send response: %w", err)
	}

	if sendResp.Error != "" {
		return "", false, fmt.Errorf("steam send trade offer error: %s", sendResp.Error)
	}

	return sendResp.TradeOfferID, sendResp.NeedsMobileConfirmation, nil
}

// AcceptTradeOfferConfirmation confirms a trade offer using Steam Guard mobile confirmations.
func (c *Client) AcceptTradeOfferConfirmation(tradeOfferID string) error {
	return c.approveConfirmationForID(tradeOfferID)
}

// AcceptTradeOfferConfirmationWithContext confirms a trade offer using Steam Guard mobile confirmations with context support.
func (c *Client) AcceptTradeOfferConfirmationWithContext(ctx context.Context, tradeOfferID string) error {
	return c.approveConfirmationForIDWithContext(ctx, tradeOfferID)
}
