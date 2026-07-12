// wtc — vendor-neutral change ledger ("git log for production").
package main

import "os"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
