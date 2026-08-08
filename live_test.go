package steam

import (
	"os"
	"testing"
)

func TestLiveAccount(t *testing.T) {
	username := os.Getenv("STEAM_USERNAME")
	password := os.Getenv("STEAM_PASSWORD")
	sharedSecret := os.Getenv("STEAM_SHARED_SECRET")
	identitySecret := os.Getenv("STEAM_IDENTITY_SECRET")
	proxyURL := os.Getenv("STEAM_PROXY")

	if username == "" {
		t.Skip("Пропуск live-теста: переменная окружения STEAM_USERNAME не задана")
	}

	cfg := ClientConfig{
		Username:       username,
		Password:       password,
		SharedSecret:   sharedSecret,
		IdentitySecret: identitySecret,
		ProxyURL:       proxyURL,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Ошибка создания клиента: %v", err)
	}

	if password != "" {
		if err := client.Login(); err != nil {
			t.Fatalf("Ошибка входа: %v", err)
		}
	}

	status, err := client.GetAccountStatus()
	if err != nil {
		t.Fatalf("Ошибка получения статуса: %v", err)
	}

	t.Logf("Успешно получен статус аккаунта %s: Баланс=%s, CS2=%d, Dota2=%d, TF2=%d",
		username, status.WalletBalance, status.CS2Count, status.Dota2Count, status.TF2Count)

	offers, err := client.GetTradeOffers(GetTradeOffersOptions{
		GetSent:     true,
		GetReceived: true,
	})
	if err != nil {
		t.Logf("Предупреждение: не удалось получить список обменов: %v", err)
	} else {
		t.Logf("Успешно получено обменов: %d", len(offers))
	}
}

