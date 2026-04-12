package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codebuddy"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type CodeBuddyIntlAuthenticator struct{}

func NewCodeBuddyIntlAuthenticator() Authenticator {
	return &CodeBuddyIntlAuthenticator{}
}

func (CodeBuddyIntlAuthenticator) Provider() string {
	return "codebuddy-intl"
}

var codeBuddyIntlRefreshLead = 24 * time.Hour

func (CodeBuddyIntlAuthenticator) RefreshLead() *time.Duration {
	return &codeBuddyIntlRefreshLead
}

func (a CodeBuddyIntlAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("codebuddy-intl: configuration is required")
	}
	if opts == nil {
		opts = &LoginOptions{}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	authSvc := codebuddy.NewCodeBuddyIntlAuth(cfg)

	authState, err := authSvc.FetchAuthState(ctx)
	if err != nil {
		return nil, fmt.Errorf("codebuddy-intl: failed to fetch auth state: %w", err)
	}

	fmt.Printf("\nPlease open the following URL in your browser to login:\n\n  %s\n\n", authState.AuthURL)
	fmt.Println("Waiting for authorization...")

	if !opts.NoBrowser {
		if browser.IsAvailable() {
			if errOpen := browser.OpenURL(authState.AuthURL); errOpen != nil {
				log.Debugf("codebuddy-intl: failed to open browser: %v", errOpen)
			}
		}
	}

	storage, err := authSvc.PollForToken(ctx, authState.State)
	if err != nil {
		return nil, fmt.Errorf("codebuddy-intl: %s: %w", codebuddy.GetUserFriendlyMessage(err), err)
	}

	fmt.Printf("\nSuccessfully logged in! (User ID: %s)\n", storage.UserID)

	authID := fmt.Sprintf("codebuddy-intl-%s.json", storage.UserID)

	label := storage.UserID
	if label == "" {
		label = "codebuddy-intl-user"
	}

	return &coreauth.Auth{
		ID:       authID,
		Provider: a.Provider(),
		FileName: authID,
		Label:    label,
		Storage:  storage,
		Metadata: map[string]any{
			"access_token":  storage.AccessToken,
			"refresh_token": storage.RefreshToken,
			"user_id":       storage.UserID,
			"domain":        storage.Domain,
			"expires_in":    storage.ExpiresIn,
			"base_url":      codebuddy.IntlBaseURL,
		},
	}, nil
}
