package main

import (
	"fmt"
	"os"

	"go-todo/internal/format"
	"go-todo/internal/store"
	"go-todo/internal/validate"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	s := store.New()
	switch os.Args[1] {
	case "add":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: add <title>")
			os.Exit(1)
		}
		title := os.Args[2]
		if err := validate.ValidateTitle(title); err != nil {
			fmt.Fprintln(os.Stderr, "invalid title:", err)
			os.Exit(1)
		}
		id := s.Add(title)
		fmt.Printf("added todo %d\n", id)
	case "list":
		fmt.Print(format.FormatList(s.List()))
	case "done":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: done <id>")
			os.Exit(1)
		}
		var id int
		fmt.Sscanf(os.Args[2], "%d", &id)
		if err := s.Done(id); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("marked %d done\n", id)
	case "pending":
		fmt.Print(format.FormatList(s.Pending()))
	default:
		usage()
	}
}

func usage() {
	fmt.Println("usage: go run . <add|list|done|pending> [args]")
}
