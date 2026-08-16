// Command oilctl 是废弃油脂回收与生物柴油出口溯源平台的命令行入口。
package main

import (
	"os"

	"wasteoil/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
