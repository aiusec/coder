package cli

import "github.com/coder/serpent"

func (r *RootCmd) ai() *serpent.Command {
	return &serpent.Command{
		Use:   "ai",
		Short: "Manage AI features.",
		Handler: func(inv *serpent.Invocation) error {
			return inv.Command.HelpHandler(inv)
		},
		Children: []*serpent.Command{
			r.aiGateway(),
		},
	}
}
