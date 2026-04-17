package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/alxxpersonal/exo-discord/internal/discord/userinstall"
	"github.com/spf13/cobra"
)

// --- User Install Mode Command ---

// newUserInstallModeCommand keeps the reserved command for script compatibility.
// Real authorization happens via the `auth` command group.
func newUserInstallModeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "user-install-mode",
		Short: "Reserved. Use `auth login` to authorize a user-install session",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(
				cmd.OutOrStdout(),
				"use `exo-discord auth login` to authorize a user-install session",
			)
			return err
		},
	}
}

// --- Types ---

type authLoginResult struct {
	OK         bool     `json:"ok"`
	UserID     string   `json:"user_id"`
	Username   string   `json:"username,omitempty"`
	Scopes     []string `json:"scopes,omitempty"`
	StorePath  string   `json:"store_path"`
	ExpiresAt  string   `json:"expires_at,omitempty"`
}

type authRecord struct {
	UserID    string   `json:"user_id"`
	Username  string   `json:"username,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

type authListResult struct {
	OAuthDir string       `json:"oauth_dir"`
	Records  []authRecord `json:"records"`
}

type authRevokeResult struct {
	OK       bool     `json:"ok"`
	UserID   string   `json:"user_id"`
	Warnings []string `json:"warnings,omitempty"`
}

// --- Auth Command ---

func newAuthCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "auth",
		Short:   "Manage user-install oauth 2.0 authorizations",
		Example: "exo-discord auth login\nexo-discord auth list\nexo-discord auth revoke <user-id>",
	}

	cmd.AddCommand(newAuthLoginCommand(env))
	cmd.AddCommand(newAuthListCommand(env))
	cmd.AddCommand(newAuthRevokeCommand(env))
	return cmd
}

// --- Login Subcommand ---

func newAuthLoginCommand(env Environment) *cobra.Command {
	var codeFlag string
	var stateFlag string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authorize a user-install oauth 2.0 session",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}
			oauthCfg, err := requireOAuthConfig(resolved)
			if err != nil {
				return err
			}
			applyOAuthOverride(&oauthCfg, env.OAuthOverride)

			client := userinstall.NewOAuthClient(oauthCfg, &http.Client{Timeout: 30 * time.Second})
			if seed := strings.TrimSpace(env.OAuthStateSeed); seed != "" {
				client.SetStateGenerator(func() (string, error) { return seed, nil })
			}
			authURL, state, err := client.AuthorizationURL()
			if err != nil {
				return err
			}

			stdout := cmd.OutOrStdout()
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "open this url to authorize:"); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), authURL); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "state:", state); err != nil {
				return err
			}

			code := strings.TrimSpace(codeFlag)
			returnedState := strings.TrimSpace(stateFlag)
			if code == "" {
				code, returnedState, err = readCallbackFromReader(cmd.InOrStdin(), cmd.ErrOrStderr())
				if err != nil {
					return err
				}
			}
			if returnedState == "" {
				return errors.New("callback did not include state")
			}

			ctx := env.commandContext()
			token, err := client.ExchangeCode(ctx, code)
			if err != nil {
				return fmt.Errorf("exchange oauth code (state preserved for retry): %w", err)
			}
			if err := client.ConsumeState(returnedState); err != nil {
				return err
			}

			bearerClient := userinstall.NewRESTClient(&http.Client{Timeout: 30 * time.Second}, env.OAuthAPIBase, token.AccessToken)
			user, err := bearerClient.CurrentUser(ctx)
			if err != nil {
				return err
			}

			storage, err := userinstall.NewStorage(resolved.OAuthDirPath)
			if err != nil {
				return err
			}

			now := time.Now().UTC()
			stored := userinstall.StoredToken{
				UserID:       user.ID,
				Username:     user.Username,
				AccessToken:  token.AccessToken,
				RefreshToken: token.RefreshToken,
				TokenType:    token.TokenType,
				Scope:        token.Scope,
				ExpiresAt:    now.Add(time.Duration(token.ExpiresIn) * time.Second),
				ObtainedAt:   now,
			}
			if err := storage.Save(stored); err != nil {
				return err
			}

			path, err := storage.PathFor(user.ID)
			if err != nil {
				return err
			}
			return writeJSON(stdout, authLoginResult{
				OK:        true,
				UserID:    user.ID,
				Username:  user.Username,
				Scopes:    splitScope(token.Scope),
				StorePath: path,
				ExpiresAt: stored.ExpiresAt.Format(time.RFC3339),
			})
		},
	}

	cmd.Flags().StringVar(&codeFlag, "code", "", "oauth authorization code from callback")
	cmd.Flags().StringVar(&stateFlag, "state", "", "oauth state value from callback")
	return cmd
}

// --- List Subcommand ---

func newAuthListCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored user-install oauth tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}
			storage, err := userinstall.NewStorage(resolved.OAuthDirPath)
			if err != nil {
				return err
			}

			ids, err := storage.List()
			if err != nil {
				return err
			}

			records := make([]authRecord, 0, len(ids))
			for _, id := range ids {
				token, err := storage.Load(id)
				if err != nil {
					continue
				}
				records = append(records, authRecord{
					UserID:    token.UserID,
					Username:  token.Username,
					Scopes:    splitScope(token.Scope),
					ExpiresAt: token.ExpiresAt.Format(time.RFC3339),
				})
			}

			return writeJSON(cmd.OutOrStdout(), authListResult{
				OAuthDir: resolved.OAuthDirPath,
				Records:  records,
			})
		},
	}
}

// --- Revoke Subcommand ---

func newAuthRevokeCommand(env Environment) *cobra.Command {
	var assumeYes bool
	var force bool

	cmd := &cobra.Command{
		Use:   "revoke <user-id>",
		Short: "Revoke and delete a stored oauth token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID := strings.TrimSpace(args[0])
			if userID == "" {
				return errors.New("user id must not be empty")
			}

			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}
			storage, err := userinstall.NewStorage(resolved.OAuthDirPath)
			if err != nil {
				return err
			}

			stored, err := storage.Load(userID)
			if err != nil {
				return err
			}

			if !assumeYes {
				confirmed, err := confirmRevocation(cmd.InOrStdin(), cmd.ErrOrStderr(), userID)
				if err != nil {
					return err
				}
				if !confirmed {
					return errors.New("revocation cancelled")
				}
			}

			oauthCfg, err := requireOAuthConfig(resolved)
			if err != nil {
				return err
			}
			applyOAuthOverride(&oauthCfg, env.OAuthOverride)

			client := userinstall.NewOAuthClient(oauthCfg, &http.Client{Timeout: 30 * time.Second})
			ctx := env.commandContext()
			stderr := cmd.ErrOrStderr()
			var warnings []string
			if stored.AccessToken != "" {
				if err := client.RevokeToken(ctx, stored.AccessToken, "access_token"); err != nil {
					msg := fmt.Sprintf("revoke access_token: %v", err)
					warnings = append(warnings, msg)
					_, _ = fmt.Fprintln(stderr, msg)
				}
			}
			if stored.RefreshToken != "" {
				if err := client.RevokeToken(ctx, stored.RefreshToken, "refresh_token"); err != nil {
					msg := fmt.Sprintf("revoke refresh_token: %v", err)
					warnings = append(warnings, msg)
					_, _ = fmt.Fprintln(stderr, msg)
				}
			}

			if len(warnings) > 0 && !force {
				return fmt.Errorf("remote revoke failed, re-run with --force to delete the local token anyway: %s", strings.Join(warnings, "; "))
			}

			if err := storage.Delete(userID); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), authRevokeResult{
				OK:       true,
				UserID:   userID,
				Warnings: warnings,
			})
		},
	}

	cmd.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "delete the local token even if remote revoke fails")
	return cmd
}

// --- Session Factory Helpers ---

// buildUserInstallSession constructs a user-install session for the selected user id.
// Selection precedence: --as-user flag, EXO_DISCORD_USER_ID env var, single stored token.
// If multiple tokens exist and no selection is supplied, returns an error listing ids.
func buildUserInstallSession(env Environment, resolved config.ResolvedConfig) (*userinstall.Session, error) {
	oauthCfg, err := requireOAuthConfig(resolved)
	if err != nil {
		return nil, err
	}
	storage, err := userinstall.NewStorage(resolved.OAuthDirPath)
	if err != nil {
		return nil, err
	}
	ids, err := storage.List()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.New("no user-install tokens stored; run `exo-discord auth login`")
	}

	selected, err := resolveUserInstallUserID(env, ids)
	if err != nil {
		return nil, err
	}

	refresher := userinstall.NewOAuthClient(oauthCfg, &http.Client{Timeout: 30 * time.Second})
	return userinstall.NewSession(userinstall.Config{
		UserID:     selected,
		Storage:    storage,
		Refresher:  refresher,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	})
}

// resolveUserInstallUserID picks a stored user id based on --as-user, EXO_DISCORD_USER_ID,
// or the single stored token. Returns a helpful error when ambiguous.
func resolveUserInstallUserID(env Environment, ids []string) (string, error) {
	selection := ""
	if env.userIDRef != nil {
		selection = strings.TrimSpace(*env.userIDRef)
	}
	if selection == "" {
		selection = strings.TrimSpace(env.UserIDOverride)
	}
	if selection == "" {
		lookup := env.LookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		if value, ok := lookup("EXO_DISCORD_USER_ID"); ok {
			selection = strings.TrimSpace(value)
		}
	}

	if selection != "" {
		for _, id := range ids {
			if id == selection {
				return id, nil
			}
		}
		return "", fmt.Errorf("user id %q has no stored token; available ids: %s", selection, strings.Join(ids, ", "))
	}

	if len(ids) == 1 {
		return ids[0], nil
	}
	return "", fmt.Errorf("multiple user-install tokens stored (%s); select one with --as-user or EXO_DISCORD_USER_ID", strings.Join(ids, ", "))
}

// --- Helpers ---

func requireOAuthConfig(resolved config.ResolvedConfig) (userinstall.OAuthConfig, error) {
	cfg := resolved.Config.OAuth
	if strings.TrimSpace(cfg.ClientID) == "" {
		return userinstall.OAuthConfig{}, errors.New("oauth.client_id is required in config")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return userinstall.OAuthConfig{}, errors.New("oauth.client_secret is required in config")
	}
	if strings.TrimSpace(cfg.RedirectURI) == "" {
		return userinstall.OAuthConfig{}, errors.New("oauth.redirect_uri is required in config")
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"identify", "guilds"}
	}
	return userinstall.OAuthConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURI:  cfg.RedirectURI,
		Scopes:       scopes,
	}, nil
}

func applyOAuthOverride(cfg *userinstall.OAuthConfig, override oauthEndpointOverride) {
	if override.AuthorizeBase != "" {
		cfg.AuthorizeBase = override.AuthorizeBase
	}
	if override.TokenBase != "" {
		cfg.TokenBase = override.TokenBase
	}
	if override.RevokeBase != "" {
		cfg.RevokeBase = override.RevokeBase
	}
}

func splitScope(scope string) []string {
	fields := strings.Fields(scope)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func readCallbackFromReader(r io.Reader, stderr io.Writer) (string, string, error) {
	reader := bufio.NewReader(r)
	if _, err := fmt.Fprint(stderr, "paste callback url or `code` value: "); err != nil {
		return "", "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", "", fmt.Errorf("read callback input: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", errors.New("callback input must not be empty")
	}

	code, state := parseCallback(line)
	if code == "" {
		return "", "", errors.New("callback input did not contain an authorization code")
	}
	return code, state, nil
}

func parseCallback(input string) (string, string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ""
	}
	if strings.Contains(trimmed, "?") || strings.HasPrefix(trimmed, "http") {
		if idx := strings.Index(trimmed, "?"); idx >= 0 {
			return parseQuery(trimmed[idx+1:])
		}
	}
	// bare values may be "code=... state=..." or just a code
	if strings.Contains(trimmed, "=") {
		return parseQuery(trimmed)
	}
	return trimmed, ""
}

func parseQuery(raw string) (string, string) {
	var code, state string
	for _, part := range splitPairs(raw) {
		key, value, ok := cutPair(part)
		if !ok {
			continue
		}
		switch key {
		case "code":
			code = value
		case "state":
			state = value
		}
	}
	return code, state
}

func splitPairs(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '&' || r == ' '
	})
	return fields
}

func cutPair(pair string) (string, string, bool) {
	idx := strings.Index(pair, "=")
	if idx < 0 {
		return "", "", false
	}
	return pair[:idx], pair[idx+1:], true
}

func confirmRevocation(r io.Reader, stderr io.Writer, userID string) (bool, error) {
	reader := bufio.NewReader(r)
	if _, err := fmt.Fprintf(stderr, "revoke oauth token for user %s? type the user id to confirm: ", userID); err != nil {
		return false, err
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	return strings.TrimSpace(line) == userID, nil
}

