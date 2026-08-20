package steam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGetTradeOffersViaAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_token") != "test_token_123" {
			t.Errorf("Expected access_token 'test_token_123', got '%s'", r.URL.Query().Get("access_token"))
		}
		if r.URL.Query().Get("get_sent_offers") != "1" || r.URL.Query().Get("get_received_offers") != "1" {
			t.Errorf("Expected get_sent_offers=1 and get_received_offers=1")
		}

		resp := econServiceOffersResponse{}
		resp.Response.TradeOffersSent = []econTradeOffer{
			{
				TradeOfferID:    "1001",
				AccountIDOther:  12345678,
				Message:         "Sending test items",
				TradeOfferState: TradeOfferStateActive,
				IsOurOffer:      true,
				ItemsToGive: []econAsset{
					{AppID: 730, ContextID: "2", AssetID: "888", Amount: "1", ClassID: "11", InstanceID: "22"},
				},
			},
		}
		resp.Response.TradeOffersReceived = []econTradeOffer{
			{
				TradeOfferID:    "1002",
				AccountIDOther:  87654321,
				Message:         "Receiving test items",
				TradeOfferState: TradeOfferStateActive,
				IsOurOffer:      false,
				ItemsToReceive: []econAsset{
					{AppID: 570, ContextID: "2", AssetID: "999", Amount: "1", ClassID: "33", InstanceID: "44"},
				},
			},
		}
		resp.Response.Descriptions = []econItemDescription{
			{AppID: 730, ClassID: "11", InstanceID: "22", MarketName: "AK-47 | Redline"},
			{AppID: 570, ClassID: "33", InstanceID: "44", MarketName: "Dragonclaw Hook"},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		Username:    "testuser",
		AccessToken: "test_token_123",
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Override HTTP Client for test pointing to mock server
	client.HTTPClient = server.Client()
	// Replace default transport with a custom transport redirecting requests to the test server
	transport := &testTransport{
		serverURL: server.URL,
	}
	client.HTTPClient.Transport = transport

	offers, err := client.GetTradeOffers(GetTradeOffersOptions{
		GetSent:         true,
		GetReceived:     true,
		GetDescriptions: true,
	})
	if err != nil {
		t.Fatalf("GetTradeOffers error: %v", err)
	}

	if len(offers) != 2 {
		t.Fatalf("Expected 2 trade offers, got %d", len(offers))
	}

	sent := offers[0]
	if sent.TradeOfferID != "1001" || !sent.IsSent || sent.PartnerSteamID != accountIDToSteamID64(12345678) {
		t.Errorf("Unexpected sent offer: %+v", sent)
	}
	if len(sent.ItemsToGive) != 1 || sent.ItemsToGive[0].Name != "AK-47 | Redline" {
		t.Errorf("Unexpected sent items: %+v", sent.ItemsToGive)
	}

	received := offers[1]
	if received.TradeOfferID != "1002" || received.IsSent || received.PartnerSteamID != accountIDToSteamID64(87654321) {
		t.Errorf("Unexpected received offer: %+v", received)
	}
	if len(received.ItemsToReceive) != 1 || received.ItemsToReceive[0].Name != "Dragonclaw Hook" {
		t.Errorf("Unexpected received items: %+v", received.ItemsToReceive)
	}
}

func TestAcceptDeclineCancelTradeOffers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/tradeoffer/1001/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>var g_ulTradePartnerSteamID = '76561198000000000';</body></html>`))
		case "/tradeoffer/1001/accept":
			if r.FormValue("sessionid") != "test_session_id" {
				t.Errorf("Expected sessionid 'test_session_id', got '%s'", r.FormValue("sessionid"))
			}
			if r.FormValue("partner") != "76561198000000000" {
				t.Errorf("Expected partner '76561198000000000', got '%s'", r.FormValue("partner"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"tradeid":"99999","needs_mobile_confirmation":false}`))
		case "/tradeoffer/1002/decline":
			if r.FormValue("sessionid") != "test_session_id" {
				t.Errorf("Expected sessionid 'test_session_id', got '%s'", r.FormValue("sessionid"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"tradeofferid":"1002"}`))
		case "/tradeoffer/1003/cancel":
			if r.FormValue("sessionid") != "test_session_id" {
				t.Errorf("Expected sessionid 'test_session_id', got '%s'", r.FormValue("sessionid"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"tradeofferid":"1003"}`))
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", 404)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	if err := client.AcceptTradeOffer("1001"); err != nil {
		t.Fatalf("AcceptTradeOffer error: %v", err)
	}

	if err := client.DeclineTradeOffer("1002"); err != nil {
		t.Fatalf("DeclineTradeOffer error: %v", err)
	}

	if err := client.CancelTradeOffer("1003"); err != nil {
		t.Fatalf("CancelTradeOffer error: %v", err)
	}
}

func TestSendTradeOffer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tradeoffer/new/send" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		r.ParseForm()
		if r.FormValue("sessionid") != "test_session_id" || r.FormValue("partner") != "76561198060265728" {
			t.Errorf("Unexpected form values: %+v", r.Form)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tradeofferid":"55555","needs_mobile_confirmation":true}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	itemsToGive := []TradeItem{
		{AssetID: "111", AppID: 730, ContextID: "2"},
	}
	itemsToReceive := []TradeItem{
		{AssetID: "222", AppID: 730, ContextID: "2"},
	}

	offerID, needsConf, err := client.SendTradeOffer("https://steamcommunity.com/tradeoffer/new/?partner=100000000&token=test_token", itemsToGive, itemsToReceive, "Hello")
	if err != nil {
		t.Fatalf("SendTradeOffer error: %v", err)
	}

	if offerID != "55555" || !needsConf {
		t.Errorf("Unexpected SendTradeOffer response: offerID=%s, needsConf=%v", offerID, needsConf)
	}
}

type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.serverURL)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestGetUserInventoryWithContext_EmptyInventoryValidSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":1,"rwgrsn":-2,"total_inventory_count":0}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	items, err := client.GetUserInventoryWithContext(nil, "730", "16")
	if err != nil {
		t.Fatalf("Expected no error for empty inventory with total_inventory_count:0, got: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("Expected 0 items, got %d", len(items))
	}
}

func TestGetUserInventoryWithContext_InvalidSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":1,"rwgrsn":-2}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	_, err = client.GetUserInventoryWithContext(nil, "730", "16")
	if err == nil {
		t.Fatalf("Expected ErrSessionExpired for response without total_inventory_count, got nil")
	}
}

func TestHasTradeProtectionModal_Present(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><body><div id="trade_protection_modal" class="modal">Trade protection</div></body></html>`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	hasModal, err := client.HasTradeProtectionModal()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !hasModal {
		t.Fatalf("Expected hasModal=true, got false")
	}
}

func TestHasTradeProtectionModal_Absent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><body><div id="other_content">No modal here</div></body></html>`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	hasModal, err := client.HasTradeProtectionModal()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if hasModal {
		t.Fatalf("Expected hasModal=false, got true")
	}
}

func TestEnsureTradeProtectionAcknowledged_WhenPresent(t *testing.T) {
	ackCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trade/new/acknowledge" {
			ackCalled = true
			if r.Method != "POST" {
				t.Errorf("Expected POST method, got %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><body><div id="trade_protection_modal">Warning</div></body></html>`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	hadModal, err := client.EnsureTradeProtectionAcknowledged()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !hadModal {
		t.Fatalf("Expected hadModal=true, got false")
	}
	if !ackCalled {
		t.Fatalf("Expected acknowledge POST to be called")
	}
}

func TestEnsureTradeProtectionAcknowledged_WhenAbsent(t *testing.T) {
	ackCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trade/new/acknowledge" {
			ackCalled = true
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><body><div>Clean page</div></body></html>`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	hadModal, err := client.EnsureTradeProtectionAcknowledged()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if hadModal {
		t.Fatalf("Expected hadModal=false, got true")
	}
	if ackCalled {
		t.Fatalf("Acknowledge POST should NOT be called when modal is absent")
	}
}