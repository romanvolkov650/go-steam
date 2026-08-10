package steam

import "context"

// MarketService defines methods for Steam Community Market interactions.
type MarketService interface {
	FetchMarketPrice(appID int, marketHashName string, currency ...int) (*MarketPriceOverview, error)
	FetchMarketPriceWithContext(ctx context.Context, appID int, marketHashName string, currency ...int) (*MarketPriceOverview, error)

	GetMarketPriceHistory(appID int, marketHashName string) ([]MarketPriceHistoryPoint, error)
	GetMarketPriceHistoryWithContext(ctx context.Context, appID int, marketHashName string) ([]MarketPriceHistoryPoint, error)

	CreateSellOrder(assetID string, appID int, contextID string, priceInCents int, amount ...int) (*CreateSellOrderResponse, error)
	CreateSellOrderWithContext(ctx context.Context, assetID string, appID int, contextID string, priceInCents int, amount ...int) (*CreateSellOrderResponse, error)

	CreateBuyOrder(appID int, marketHashName string, priceInCents int, quantity int, currency ...int) (*CreateBuyOrderResponse, error)
	CreateBuyOrderWithContext(ctx context.Context, appID int, marketHashName string, priceInCents int, quantity int, currency ...int) (*CreateBuyOrderResponse, error)

	CancelSellOrder(listingID string) error
	CancelSellOrderWithContext(ctx context.Context, listingID string) error

	CancelBuyOrder(buyOrderID string) error
	CancelBuyOrderWithContext(ctx context.Context, buyOrderID string) error

	GetMyMarketListings() ([]*MarketListing, error)
	GetMyMarketListingsWithContext(ctx context.Context) ([]*MarketListing, error)

	GetMyBuyOrders() ([]*MarketBuyOrder, error)
	GetMyBuyOrdersWithContext(ctx context.Context) ([]*MarketBuyOrder, error)

	AcceptMarketListingConfirmation(listingID string) error
	AcceptMarketListingConfirmationWithContext(ctx context.Context, listingID string) error
}

// TradeOfferService defines methods for Steam Trade Offer operations.
type TradeOfferService interface {
	GetTradeOffers(opts GetTradeOffersOptions) ([]*TradeOffer, error)
	GetTradeOffersWithContext(ctx context.Context, opts GetTradeOffersOptions) ([]*TradeOffer, error)

	GetUserInventory(appID, contextID string) ([]TradeItem, error)
	GetUserInventoryWithContext(ctx context.Context, appID, contextID string) ([]TradeItem, error)

	GetPartnerInventory(steamID, appID, contextID string) ([]TradeItem, error)
	GetPartnerInventoryWithContext(ctx context.Context, steamID, appID, contextID string) ([]TradeItem, error)

	GetTradeOffer(tradeOfferID string, opts ...GetTradeOffersOptions) (*TradeOffer, error)
	GetTradeOfferWithContext(ctx context.Context, tradeOfferID string, opts ...GetTradeOffersOptions) (*TradeOffer, error)

	AcceptTradeOffer(tradeOfferID string, partnerSteamID ...string) error
	AcceptTradeOfferWithContext(ctx context.Context, tradeOfferID string, partnerSteamID ...string) error

	DeclineTradeOffer(tradeOfferID string) error
	DeclineTradeOfferWithContext(ctx context.Context, tradeOfferID string) error

	CancelTradeOffer(tradeOfferID string) error
	CancelTradeOfferWithContext(ctx context.Context, tradeOfferID string) error

	SendTradeOffer(tradeURL string, itemsToGive, itemsToReceive []TradeItem, message string) (string, bool, error)
	SendTradeOfferWithContext(ctx context.Context, tradeURL string, itemsToGive, itemsToReceive []TradeItem, message string) (string, bool, error)

	AcceptTradeOfferConfirmation(tradeOfferID string) error
	AcceptTradeOfferConfirmationWithContext(ctx context.Context, tradeOfferID string) error
}

// AuthenticatorService defines methods for Steam Guard 2FA and mobile confirmations.
type AuthenticatorService interface {
	Generate2FACode() (string, error)
	GetConfirmations() ([]*Confirmation, error)
	GetConfirmationsWithContext(ctx context.Context) ([]*Confirmation, error)
	SendConfirmationAction(conf *Confirmation, action string) error
	SendConfirmationActionWithContext(ctx context.Context, conf *Confirmation, action string) error
}

// AccountService defines methods for Steam authentication and account state.
type AccountService interface {
	Login() error
	LoginWithContext(ctx context.Context) error
	LoginWithRefreshToken() error
	LoginWithRefreshTokenWithContext(ctx context.Context) error
	GetAccountStatus() (*AccountStatus, error)
	GetAccountStatusWithContext(ctx context.Context) (*AccountStatus, error)
	GetTradeURL() (string, error)
	GetTradeURLWithContext(ctx context.Context) (string, error)
	GetAvatarURL() (string, error)
	GetAvatarURLWithContext(ctx context.Context) (string, error)
	FetchAccountDetails() (*FullAccountDetails, error)
	FetchAccountDetailsWithContext(ctx context.Context) (*FullAccountDetails, error)
	ExportCookies() ([]*CookieJSON, error)
	ExportCookiesJSON() (string, error)
	ImportCookies(cookies []*CookieJSON)
	ImportCookiesJSON(jsonStr string) error
	SaveCookiesToFile(filePath string) error
	LoadCookiesFromFile(filePath string) error
}

// SteamClient combines all service interfaces into a unified contract.
type SteamClient interface {
	MarketService
	TradeOfferService
	AuthenticatorService
	AccountService
}

// Compile-time interface check
var _ SteamClient = (*Client)(nil)
