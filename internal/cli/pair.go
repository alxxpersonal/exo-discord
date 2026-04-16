package cli

import (
	"time"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/spf13/cobra"
)

// --- Types ---

type pendingPairRecord struct {
	Code        string `json:"code"`
	SenderID    string `json:"sender_id"`
	ChatID      string `json:"chat_id"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at"`
	ResendCount int    `json:"resend_count"`
}

type pendingPairsResult struct {
	PendingPairs []pendingPairRecord `json:"pending_pairs"`
}

type pairMutationResult struct {
	OK      bool              `json:"ok"`
	Code    string            `json:"code"`
	Pending pendingPairRecord `json:"pending,omitempty"`
}

// --- Pair Commands ---

func newPairCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pair",
		Short:   "Inspect and resolve pending pairings",
		Example: "exo-discord pair list\nexo-discord pair approve AB12CD34\nexo-discord pair deny AB12CD34",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairList(cmd, env)
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List pending pairing codes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairList(cmd, env)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "approve <code>",
		Short: "Approve a pending pairing code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			manager, err := access.NewManager(resolved.AccessStatePath, accessPolicyFromConfig(resolved.Config))
			if err != nil {
				return err
			}

			pending, err := manager.Approve(args[0])
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), pairMutationResult{
				OK:      true,
				Code:    args[0],
				Pending: newPendingPairRecord(pending),
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "deny <code>",
		Short: "Deny a pending pairing code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			manager, err := access.NewManager(resolved.AccessStatePath, accessPolicyFromConfig(resolved.Config))
			if err != nil {
				return err
			}

			if err := manager.Deny(args[0]); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), pairMutationResult{
				OK:   true,
				Code: args[0],
			})
		},
	})

	return cmd
}

// --- Helpers ---

func runPairList(cmd *cobra.Command, env Environment) error {
	resolved, err := env.resolveConfig()
	if err != nil {
		return err
	}

	manager, err := access.NewManager(resolved.AccessStatePath, accessPolicyFromConfig(resolved.Config))
	if err != nil {
		return err
	}

	pairs := manager.PendingPairs()
	result := make([]pendingPairRecord, 0, len(pairs))
	for _, pending := range pairs {
		result = append(result, newPendingPairRecord(pending))
	}

	return writeJSON(cmd.OutOrStdout(), pendingPairsResult{
		PendingPairs: result,
	})
}

func newPendingPairRecord(pending access.PendingPair) pendingPairRecord {
	return pendingPairRecord{
		Code:        pending.Code,
		SenderID:    pending.SenderID,
		ChatID:      pending.ChatID,
		CreatedAt:   pending.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:   pending.ExpiresAt.UTC().Format(time.RFC3339),
		ResendCount: pending.ResendCount,
	}
}
