package main

import (
	"os"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Paginated list of documents",
	Run: func(cmd *cobra.Command, args []string) {
		idx, err := index.NewStore()
		if err != nil {
			ui.PrintErr("failed to open index", err.Error(), nil)
			os.Exit(1)
		}

		entries := idx.List()
		
		if len(entries) == 0 {
			ui.PrintInfo("no documents found", "", nil)
			os.Exit(0)
		}

		ui.Heading("documents", nil, nil)

		var rows [][]interface{}
		for _, e := range entries {
			sizeStr := humanize.Bytes(uint64(e.Size))
			if e.State == index.StatePlaceholder {
				sizeStr = "-"
			}
			rows = append(rows, []interface{}{e.ID, e.State, sizeStr})
		}

		// Apply semantic colors to the state column (column 1)
		colorFn := func(cell string) string {
			switch index.DocumentState(cell) {
			case index.StateCached:
				return ui.Ok
			case index.StatePlaceholder:
				return ui.Info
			case index.StateStale:
				return ui.Warn
			case index.StateDirty, index.StateDiverged:
				return ui.Err
			case index.StateNew:
				return ui.Accent
			default:
				return ""
			}
		}

		// Color column index 1, dim column 2 (size)
		ui.Table(rows, &ui.ColorCol{Index: 1, Fn: colorFn}, []int{2}, nil)
	},
}


