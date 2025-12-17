package main

import (
	dokkuproperty "dokku-nginx-custom/src/pkg/dokku_property"
	"fmt"
	"log"
	"os"

	flag "github.com/spf13/pflag"
)

func main() {
	args := flag.NewFlagSet("ps:inspect", flag.ExitOnError)
	appName := args.String("app", "", "app: the app to inspect")
	_ = args.Bool("global", false, "global: inspect global property")
	_ = args.Bool("computed", false, "computed: inspect computed property")
	logRoot := args.String("log-root", "", "log-root: log root directory")
	err := args.Parse(os.Args[1:])
	if err != nil {
		log.Fatalln(err)
	}

	if *appName == "" {
		log.Fatalf("app is required. args: %s\n", os.Args)
	}

	if *logRoot == "" {
		log.Fatalf("log-root is required. args: %s\n", os.Args)
	}

	proxyName := os.Getenv("PROXY_NAME")
	if proxyName == "" {
		log.Fatalln("PROXY_NAME environment variable is required")
	}

	property := args.Arg(0)
	fmt.Print(dokkuproperty.GetComputedProperty(*appName, property))
}
