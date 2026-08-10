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

	cookiesFile := "cookies_steamuhd.json"
	loaded := false
	if _, statErr := os.Stat(cookiesFile); statErr == nil {
		if loadErr := client.LoadCookiesFromFile(cookiesFile); loadErr == nil {
			if alive, _ := client.IsSessionAlive(); alive {
				t.Log("Успешно загружены куки, сессия жива. Пропускаем Login по паролю.")
				loaded = true
			}
		}
	}

	if !loaded && password != "" {
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

	// Verify SaveCookiesToFile and LoadCookiesFromFile
	tmpCookiesFile := t.TempDir() + "/live_cookies.json"
	if err := client.SaveCookiesToFile(tmpCookiesFile); err != nil {
		t.Fatalf("Ошибка сохранения кук в файл: %v", err)
	}
	t.Logf("Успешно сохранены куки в %s", tmpCookiesFile)

	newClient, err := NewClient(ClientConfig{
		Username: username,
		ProxyURL: proxyURL,
	})
	if err != nil {
		t.Fatalf("Ошибка создания нового клиента для проверки кук: %v", err)
	}

	if err := newClient.LoadCookiesFromFile(tmpCookiesFile); err != nil {
		t.Fatalf("Ошибка загрузки кук из файла: %v", err)
	}
	t.Logf("Успешно загружены куки из файла")

	// Verify the new client is logged in and can fetch status using loaded cookies
	newStatus, err := newClient.GetAccountStatus()
	if err != nil {
		t.Fatalf("Ошибка получения статуса с загруженными куками: %v", err)
	}

	if newStatus.WalletBalance != status.WalletBalance {
		t.Errorf("Несовпадение баланса: %s (оригинал) vs %s (с куками из файла)", status.WalletBalance, newStatus.WalletBalance)
	} else {
		t.Logf("Баланс с загруженными куками совпадает: %s", newStatus.WalletBalance)
	}
}

