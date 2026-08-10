package steam

import (
	"testing"
)

func TestExtractWalletBalanceDetails(t *testing.T) {
	htmlSnippet := `<a class="global_action_link" id="header_wallet_balance" href="https://store.steampowered.com/account/store_transactions/">5 968,85₴<br><span class="tooltip steam" data-tooltip-html="Some funds are pending. This typically occurs after a purchase you made has been refunded. These Steam Wallet funds are currently unusable.&lt;br&gt;&lt;br&gt;(available in 1-2 days)">Pending: 440,52₴</span></a>`

	bal, pendingBal, avail := extractWalletBalanceDetailsFromHTML(htmlSnippet)

	t.Logf("Extracted balance: '%s'", bal)
	t.Logf("Extracted pending balance: '%s'", pendingBal)
	t.Logf("Extracted pending availability: '%s'", avail)

	if bal != "5 968,85₴" {
		t.Errorf("Expected balance '5 968,85₴', got '%s'", bal)
	}
	if pendingBal != "440,52₴" && pendingBal != "Pending: 440,52₴" {
		t.Errorf("Expected pending balance '440,52₴', got '%s'", pendingBal)
	}
	if avail != "available in 1-2 days" {
		t.Errorf("Expected availability 'available in 1-2 days', got '%s'", avail)
	}
}

func TestExtractWalletBalanceDetailsNoPending(t *testing.T) {
	htmlSnippet := `<a class="global_action_link" id="header_wallet_balance" href="https://store.steampowered.com/account/store_transactions/">1 250,50₴</a>`

	bal, pendingBal, avail := extractWalletBalanceDetailsFromHTML(htmlSnippet)

	if bal != "1 250,50₴" {
		t.Errorf("Expected balance '1 250,50₴', got '%s'", bal)
	}
	if pendingBal != "" {
		t.Errorf("Expected empty pending balance, got '%s'", pendingBal)
	}
	if avail != "" {
		t.Errorf("Expected empty availability, got '%s'", avail)
	}
}
