package cli

import (
	"encoding/json"
	"fmt"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/spf13/cobra"
)

func newConversationsCommand(getService func() (*app.Service, error)) *cobra.Command {
	cmd := &cobra.Command{Use: "conversations", Short: "Manage local conversations"}
	cmd.AddCommand(&cobra.Command{Use: "create", Args: cobra.NoArgs, Short: "Create a conversation", RunE: func(c *cobra.Command, _ []string) error {
		s, e := getService()
		if e != nil {
			return e
		}
		v, e := s.CreateConversation(c.Context())
		if e != nil {
			return e
		}
		_, e = fmt.Fprintln(c.OutOrStdout(), v.ID)
		return e
	}})
	cmd.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List conversations", RunE: func(c *cobra.Command, _ []string) error {
		s, e := getService()
		if e != nil {
			return e
		}
		items, e := s.ListConversations(c.Context())
		if e != nil {
			return e
		}
		for _, v := range items {
			if _, e = fmt.Fprintf(c.OutOrStdout(), "%s\t%d messages\t%s\n", v.ID, v.MessageCount, v.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")); e != nil {
				return e
			}
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), Short: "Show a conversation transcript", RunE: func(c *cobra.Command, args []string) error {
		s, e := getService()
		if e != nil {
			return e
		}
		v, e := s.GetConversation(c.Context(), args[0])
		if e != nil {
			return e
		}
		enc := json.NewEncoder(c.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}})
	cmd.AddCommand(&cobra.Command{Use: "delete ID", Args: cobra.ExactArgs(1), Short: "Delete a conversation", RunE: func(c *cobra.Command, args []string) error {
		s, e := getService()
		if e != nil {
			return e
		}
		return s.DeleteConversation(c.Context(), args[0])
	}})
	return cmd
}
