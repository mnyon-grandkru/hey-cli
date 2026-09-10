package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/basecamp/hey-sdk/go/pkg/generated"
	hey "github.com/basecamp/hey-sdk/go/pkg/hey"

	"github.com/basecamp/hey-cli/internal/apierr"
	"github.com/basecamp/hey-cli/internal/output"
	"github.com/basecamp/hey-cli/internal/terminal"
)

// resolveSenderID maps a From email to an identity sender ID (HEY Connect-an-Address /
// linked mailbox identities appear on GET /identity.json as senders[]).
func resolveSenderID(ctx context.Context, email string) (int64, error) {
	identity, err := rootSDK.Identity().GetIdentity(ctx)
	if err != nil {
		return 0, apierr.FromSDK(err)
	}
	if identity == nil {
		return 0, apierr.ErrAPI(0, "HEY returned no identity data")
	}

	email = strings.ToLower(strings.TrimSpace(email))
	available := senderEmails(identity.Senders)
	for _, s := range identity.Senders {
		if strings.ToLower(strings.TrimSpace(s.EmailAddress)) == email {
			return s.Id, nil
		}
	}

	if len(available) == 0 {
		return 0, apierr.ErrUsage(fmt.Sprintf(
			"no sender matching %q (identity has no senders — Connect-an-Address identities should appear on GET /identity.json senders[])",
			email,
		))
	}
	return 0, apierr.ErrUsage(fmt.Sprintf(
		"no sender matching %q (available: %s)",
		email, strings.Join(available, ", "),
	))
}

// createMessageAsSender delivers a new message as actingSenderID. Zero uses the
// account default via Messages().Create; a non-zero id is the Connect-an-Address
// / identity sender resolved from --from.
func createMessageAsSender(ctx context.Context, actingSenderID int64, subject, content string, to, cc, bcc []string) error {
	if actingSenderID == 0 {
		return apierr.FromSDK(sdk.Messages().Create(ctx, subject, content, to, cc, bcc))
	}
	draft := hey.DraftContent{
		Subject:        subject,
		Content:        content,
		To:             to,
		CC:             cc,
		BCC:            bcc,
		ActingSenderID: actingSenderID,
	}
	entryID, err := sdk.Messages().CreateDraft(ctx, draft)
	if err != nil {
		return apierr.FromSDK(err)
	}
	return apierr.FromSDK(sdk.Messages().SendDraft(ctx, entryID, draft))
}

func senderEmails(senders []generated.Sender) []string {
	seen := map[string]struct{}{}
	var available []string
	for _, s := range senders {
		addr := strings.TrimSpace(s.EmailAddress)
		if addr == "" {
			continue
		}
		key := strings.ToLower(addr)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		available = append(available, addr)
	}
	sort.Strings(available)
	return available
}

type senderListItem struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	AccountID int64  `json:"account_id,omitempty"`
	Default   bool   `json:"default"`
	Type      string `json:"contactable_type,omitempty"`
}

func newSendersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "senders",
		Short: "List From identities (including Connect-an-Address)",
		Long:  "List the sender identities HEY will accept as acting_sender_id — primary mailbox plus Connect-an-Address aliases. These are not the same as hey account list linked mail accounts.",
		Annotations: map[string]string{
			"agent_notes": "Use before hey compose --from <email>. Returns identity.senders from GET /identity.json. Connect-an-Address addresses (e.g. mark@grandkru.com) appear here even when hey account list only shows the primary hey.com mailbox.",
		},
	}
	cmd.AddCommand(newSendersListCommand())
	return cmd
}

func newSendersListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available From / acting sender emails",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireAuth(); err != nil {
				return err
			}
			identity, err := rootSDK.Identity().GetIdentity(cmd.Context())
			if err != nil {
				return apierr.FromSDK(err)
			}
			if identity == nil {
				return apierr.ErrAPI(0, "HEY returned no identity data")
			}
			items := make([]senderListItem, 0, len(identity.Senders))
			for _, s := range identity.Senders {
				items = append(items, senderListItem{
					ID:        s.Id,
					Email:     s.EmailAddress,
					Name:      s.Name,
					AccountID: s.AccountId,
					Default:   s.Default,
					Type:      s.ContactableType,
				})
			}
			if writer.IsStyled() {
				table := newTable(cmd.OutOrStdout())
				table.addRow([]string{"ID", "Email", "Name", "Account", "Default", "Type"})
				for _, item := range items {
					def := ""
					if item.Default {
						def = "yes"
					}
					table.addRow([]string{
						fmt.Sprintf("%d", item.ID),
						terminal.SanitizeLine(item.Email),
						terminal.SanitizeLine(item.Name),
						fmt.Sprintf("%d", item.AccountID),
						def,
						terminal.SanitizeLine(item.Type),
					})
				}
				table.print()
				return nil
			}
			return writeOK(items, output.WithSummary(fmt.Sprintf("%d senders", len(items))))
		},
	}
}
