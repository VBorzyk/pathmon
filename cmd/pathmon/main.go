package main

import (
	"fmt"
	"os"
)

// version подставляется при сборке через -ldflags (см. Makefile).
// Если собрать обычной командой go build, останется значение "dev".
var version = "dev"

func main() {
	// os.Args — срез строк с аргументами командной строки.
	// os.Args[0] — это всегда путь к самой программе.
	// Значит первый аргумент пользователя лежит в os.Args[1].
	//
	// Если пользователь набрал просто "pathmon", длина среза равна 1,
	// команды нет — печатаем справку и выходим с кодом 2.
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	command := os.Args[1]

	switch command {
	case "watch":
		runWatch()
	case "version":
		fmt.Println("pathmon", version)
	case "help":
		printUsage()
	default:
		// Fprintf, а не Printf: пишем в os.Stderr — поток ошибок.
		// Это важно: если пользователь перенаправит вывод в файл
		// (pathmon watch > log.txt), сообщения об ошибках всё равно
		// останутся видны на экране.
		fmt.Fprintf(os.Stderr, "pathmon: unknown command %q\n\n", command)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`pathmon monitors host reachability and reports problems.

Usage:
  pathmon <command>

Команды:
  watch     run continuous monitoring
  version   print version information
  help      show this help`)
}

// runWatch пока заглушка. Наполним её 24 августа.
func runWatch() {
	fmt.Println("watch: not implemented yet")
}
