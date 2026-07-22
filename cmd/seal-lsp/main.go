package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"

	"seal/internal/lsp"
)

func main() {
	currentDirectory, err :=
		os.Getwd()

	if err != nil {
		log.Fatalf(
			"determining current directory: %v",
			err,
		)
	}

	defaultWorkspace :=
		flag.String(
			"workspace",
			currentDirectory,
			"default workspace path when the client does not provide one",
		)

	flag.Parse()

	logger :=
		log.New(
			os.Stderr,
			"seal-lsp: ",
			log.LstdFlags,
		)

	transport :=
		lsp.NewTransport(
			os.Stdin,
			os.Stdout,
		)

	server :=
		lsp.NewServer(
			transport,
			lsp.ServerOptions{
				DefaultRoot: *defaultWorkspace,

				Logger: logger,

				Name: "Seal Language Server",

				Version: "0.1.0",
			},
		)

	err =
		server.Serve(
			context.Background(),
		)

	if err == nil {
		return
	}

	var exitError *lsp.ExitError

	if errors.As(
		err,
		&exitError,
	) {
		os.Exit(
			exitError.Code,
		)
	}

	logger.Printf(
		"server stopped: %v",
		err,
	)

	os.Exit(1)
}
