package managecmd

import (
	"context"
	"os"
	"os/signal"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- Interaction Commands ---

func newInteractionsCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interactions",
		Short: "Manage Discord interactions",
	}

	cmd.AddCommand(newInteractionsListenCommand(env))
	cmd.AddCommand(newInteractionsRespondCommand(env))

	return cmd
}

func newInteractionsListenCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "listen",
		Short: "Listen for interaction events and print them as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.ResolveConfig()
			if err != nil {
				return err
			}

			manager, err := env.NewManager(resolved)
			if err != nil {
				return err
			}

			baseCtx := env.CommandContext()
			ctx, stop := signal.NotifyContext(baseCtx, os.Interrupt)
			defer stop()

			if err := manager.Open(ctx); err != nil {
				return err
			}
			defer func() {
				_ = manager.Close(ctx)
			}()

			events := make(chan discord.InteractionEvent, 16)
			unsubscribe := manager.SubscribeInteractions(func(_ context.Context, event discord.InteractionEvent) {
				select {
				case events <- event:
				default:
				}
			})
			defer unsubscribe()

			for {
				select {
				case <-ctx.Done():
					return nil
				case event := <-events:
					if err := writeJSON(cmd.OutOrStdout(), event); err != nil {
						return err
					}
				}
			}
		},
	}
}

func newInteractionsRespondCommand(env Environment) *cobra.Command {
	var (
		interactionID string
		token         string
		responseType  int
		body          string
	)

	cmd := &cobra.Command{
		Use:   "respond",
		Short: "Send a raw interaction callback response",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := parseJSON(body)
			if err != nil {
				return err
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.RespondInteraction(ctx, discord.InteractionResponseRequest{
					InteractionID:    interactionID,
					InteractionToken: token,
					Type:             responseType,
					Body:             payload,
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":             true,
					"interaction_id": interactionID,
					"type":           responseType,
				})
			})
		},
	}

	cmd.Flags().StringVar(&interactionID, "id", "", "discord interaction id")
	cmd.Flags().StringVar(&token, "token", "", "discord interaction token")
	cmd.Flags().IntVar(&responseType, "type", 4, "interaction response type")
	cmd.Flags().StringVar(&body, "body", "", "json payload for the interaction response data object")
	markRequired(cmd, "id")
	markRequired(cmd, "token")
	markRequired(cmd, "type")
	return cmd
}
