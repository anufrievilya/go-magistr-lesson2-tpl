package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	// Проверяем, передан ли аргумент с именем файла
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Использование: %s <имя_файла>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]

	// Открываем файл
	file, err := os.Open(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка при открытии файла '%s': %v\n", filename, err)
		os.Exit(1)
	}
	defer file.Close()

	// Читаем содержимое файла (просто для демонстрации)
	_, err = io.ReadAll(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка при чтении файла '%s': %v\n", filename, err)
		os.Exit(1)
	}

	// Выводим сообщение в stderr
	fmt.Fprintf(os.Stderr, "Файл '%s' успешно принят и обработан\n", filename)
}
