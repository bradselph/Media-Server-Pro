// Package mediaserver provides an embeddable media server that can be imported
// as a Go library. Instead of running the standalone binary, consumers can
// construct a fully-wired server with functional options and embed it in their
// own applications.
//
// Basic usage:
//
//	srv, err := mediaserver.New(
//	    mediaserver.WithConfigPath("config.json"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if err := srv.ListenAndServe(); err != nil {
//	    log.Fatal(err)
//	}
//
// Cherry-pick modules:
//
//	srv, err := mediaserver.New(
//	    mediaserver.WithConfigPath("config.json"),
//	    mediaserver.WithModuleSet(mediaserver.CoreModules),
//	)
//
// Custom module selection:
//
//	srv, err := mediaserver.New(
//	    mediaserver.WithConfigPath("config.json"),
//	    mediaserver.WithModules(
//	        mediaserver.ModMedia,
//	        mediaserver.ModStreaming,
//	        mediaserver.ModHLS,
//	        mediaserver.ModAuth,
//	    ),
//	)
package mediaserver
