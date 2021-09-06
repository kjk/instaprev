package main

import "flag"

func doRunServer() {
	logf(ctx(), "Hello, world!\n")
}

func main() {
	var (
		flgRun bool
	)
	{
		flag.BoolVar(&flgRun, "run", false, "run the server")
		flag.Parse()
	}
	if flgRun {
		doRunServer()
		return
	}
	flag.Usage()
}
