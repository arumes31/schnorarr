package notification

import "testing"

func TestValidateDiscordWebhook(t *testing.T) {
	valid := []string{
		"",
		"https://discord.com/api/webhooks/123/abc",
		"https://discordapp.com/api/webhooks/123/abc",
	}
	for _, u := range valid {
		if err := ValidateDiscordWebhook(u); err != nil {
			t.Errorf("%q rejected: %v", u, err)
		}
	}
	invalid := []string{
		"http://discord.com/api/webhooks/123/abc",
		"https://169.254.169.254/latest/meta-data",
		"https://discord.com.evil.example/api/webhooks/1/a",
		"https://discord.com:8443/api/webhooks/1/a",
		"https://user@discord.com/api/webhooks/1/a",
		"https://discord.com/other",
		"not a url",
	}
	for _, u := range invalid {
		if err := ValidateDiscordWebhook(u); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	if httpClient.CheckRedirect == nil || httpClient.Timeout == 0 {
		t.Fatal("notification client must have a timeout and refuse redirects")
	}
}
