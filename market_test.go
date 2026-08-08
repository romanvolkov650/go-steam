package steam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchMarketPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/market/priceoverview/" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("appid") != "730" || r.URL.Query().Get("market_hash_name") != "AK-47 | Redline (Field-Tested)" {
			t.Errorf("Unexpected query params: %s", r.URL.RawQuery)
		}

		resp := MarketPriceOverview{
			Success:     true,
			LowestPrice: "1250,50 pуб.",
			Volume:      "1,500",
			MedianPrice: "1240,00 pуб.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	price, err := client.FetchMarketPrice(730, "AK-47 | Redline (Field-Tested)", 5)
	if err != nil {
		t.Fatalf("FetchMarketPrice error: %v", err)
	}

	if !price.Success || price.LowestPrice != "1250,50 pуб." || price.Volume != "1,500" {
		t.Errorf("Unexpected price overview response: %+v", price)
	}
}

func TestCreateSellAndBuyOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/market/sellitem/":
			if r.FormValue("sessionid") != "test_session_id" || r.FormValue("assetid") != "999888" || r.FormValue("price") != "5000" {
				t.Errorf("Unexpected sell form values: %+v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"success":true,"requires_confirmation":true,"listingid":"777111"}`))
		case "/market/createbuyorder/":
			if r.FormValue("sessionid") != "test_session_id" || r.FormValue("appid") != "730" || r.FormValue("price_total") != "4500" {
				t.Errorf("Unexpected buy form values: %+v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"success":1,"buy_orderid":"888222"}`))
		case "/market/removelisting/777111":
			if r.FormValue("sessionid") != "test_session_id" {
				t.Errorf("Unexpected cancel sell form: %+v", r.Form)
			}
			w.WriteHeader(http.StatusOK)
		case "/market/cancelbuyorder/":
			if r.FormValue("buy_order_id") != "888222" {
				t.Errorf("Unexpected cancel buy form: %+v", r.Form)
			}
			w.WriteHeader(http.StatusOK)
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

	// 1. Create Sell Order
	sellResp, err := client.CreateSellOrder("999888", 730, "2", 5000)
	if err != nil {
		t.Fatalf("CreateSellOrder error: %v", err)
	}
	if !sellResp.Success || sellResp.ListingID != "777111" || !bool(sellResp.RequiresConfirmation) {
		t.Errorf("Unexpected sell response: %+v", sellResp)
	}

	// 2. Create Buy Order
	buyResp, err := client.CreateBuyOrder(730, "AK-47 | Redline (Field-Tested)", 4500, 1, 5)
	if err != nil {
		t.Fatalf("CreateBuyOrder error: %v", err)
	}
	if buyResp.BuyOrderID != "888222" {
		t.Errorf("Expected buyOrderID '888222', got '%s'", buyResp.BuyOrderID)
	}

	// 3. Cancel Sell Order
	if err := client.CancelSellOrder("777111"); err != nil {
		t.Fatalf("CancelSellOrder error: %v", err)
	}

	// 4. Cancel Buy Order
	if err := client.CancelBuyOrder("888222"); err != nil {
		t.Fatalf("CancelBuyOrder error: %v", err)
	}
}

func TestGetMyMarketListingsAndBuyOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/market/mylistings/render/" {
			resp := myListingsRenderResponse{
				Success:       true,
				ResultsHTML:   `<div id="mylisting_543398165555963127">Sell Listing</div>`,
				BuyOrdersHTML: `<div id="mybuyorder_8605059000"><a href="javascript:CancelMarketBuyOrder('8605059000')">Cancel</a></div>`,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	client.SessionID = "test_session_id"
	client.HTTPClient.Transport = &testTransport{serverURL: server.URL}

	listings, err := client.GetMyMarketListings()
	if err != nil {
		t.Fatalf("GetMyMarketListings error: %v", err)
	}
	if len(listings) != 1 || listings[0].ListingID != "543398165555963127" {
		t.Errorf("Expected 1 listing with ID 543398165555963127, got %+v", listings)
	}

	buyOrders, err := client.GetMyBuyOrders()
	if err != nil {
		t.Fatalf("GetMyBuyOrders error: %v", err)
	}
	if len(buyOrders) != 1 || buyOrders[0].BuyOrderID != "8605059000" {
		t.Errorf("Expected 1 buy order with ID 8605059000, got %+v", buyOrders)
	}
}
