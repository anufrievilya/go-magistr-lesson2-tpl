package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Регулярные выражения для валидации различных форматов
// Почему-то не второй раз
var (
	// Проверка формата snake_case (маленькие буквы, цифры, подчеркивания)
	snakeCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	// Проверка формата памяти (число + единица измерения Gi/Mi/Ki)
	memoryRegex = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)

	// Проверка формата образа (должен быть из домена registry.bigbrother.io с тегом)
	imageRegex = regexp.MustCompile(`^registry\.bigbrother\.io/[^:]+:.+$`)

	// Проверка абсолютного пути (должен начинаться с /)
	absolutePathRegex = regexp.MustCompile(`^/`)
)

// Validator - структура для хранения информации об ошибках и имени файла
type Validator struct {
	filename string   // Имя проверяемого файла
	errors   []string // Список найденных ошибок
}

// NewValidator - создание нового валидатора
func NewValidator(filename string) *Validator {
	return &Validator{
		filename: filename,
		errors:   []string{},
	}
}

// addError - добавление ошибки в список
// Если line > 0, добавляется номер строки, иначе только сообщение
func (v *Validator) addError(line int, message string) {
	if line > 0 {
		// Формат с номером строки: "filename:line message"
		v.errors = append(v.errors, fmt.Sprintf("%s:%d %s", v.filename, line, message))
	} else {
		// Формат без номера строки: "filename message"
		v.errors = append(v.errors, fmt.Sprintf("%s %s", v.filename, message))
	}
}

// hasErrors - проверка наличия ошибок
func (v *Validator) hasErrors() bool {
	return len(v.errors) > 0
}

// printErrors - вывод всех ошибок в stderr
func (v *Validator) printErrors() {
	for _, err := range v.errors {
		fmt.Fprintln(os.Stderr, err)
	}
}

// parseMap - преобразование YAML узла в map для удобного доступа к полям
// YAML хранит пары ключ-значение последовательно: [key1, value1, key2, value2, ...]
// Эта функция преобразует их в map[string]*yaml.Node для удобного доступа
func (v *Validator) parseMap(node *yaml.Node) map[string]*yaml.Node {
	result := make(map[string]*yaml.Node)

	// Проверка что node и его содержимое не nil (защита от паники)
	if node == nil || node.Content == nil {
		return result
	}

	// Проходим по парам: ключ (четный индекс), значение (нечетный индекс)
	for i := 0; i < len(node.Content)-1; i += 2 {
		// Дополнительная проверка что элементы существуют
		if node.Content[i] != nil && node.Content[i+1] != nil {
			key := node.Content[i].Value
			value := node.Content[i+1]
			result[key] = value
		}
	}
	return result
}

// Validate - главная функция валидации, проверяет весь YAML документ
func (v *Validator) Validate(root *yaml.Node) {
	// Проверка что root не nil и файл не пустой
	if root == nil || len(root.Content) == 0 {
		v.addError(0, "empty YAML file")
		return
	}

	// Получаем корневой документ (первый элемент)
	doc := root.Content[0]

	// Дополнительная проверка на nil
	if doc == nil {
		v.addError(0, "invalid document")
		return
	}

	// Преобразуем документ в map для удобства работы
	fields := v.parseMap(doc)

	// Запускаем валидацию полей верхнего уровня
	v.validateTopLevel(fields)
}

// validateTopLevel - валидация полей верхнего уровня (apiVersion, kind, metadata, spec)
func (v *Validator) validateTopLevel(fields map[string]*yaml.Node) {
	// Проверка apiVersion - обязательное поле, должно быть "v1"
	if apiVersion, ok := fields["apiVersion"]; !ok {
		// Поле отсутствует
		v.addError(0, "apiVersion is required")
	} else if apiVersion.Value != "v1" {
		// Поле есть, но значение неправильное
		v.addError(apiVersion.Line, "apiVersion has unsupported value '"+apiVersion.Value+"'")
	}

	// Проверка kind - обязательное поле, должно быть "Pod"
	if kind, ok := fields["kind"]; !ok {
		v.addError(0, "kind is required")
	} else if kind.Value != "Pod" {
		v.addError(kind.Line, "kind has unsupported value '"+kind.Value+"'")
	}

	// Проверка metadata - обязательное поле, должно быть объектом
	if metadata, ok := fields["metadata"]; !ok {
		v.addError(0, "metadata is required")
	} else {
		// Валидируем содержимое metadata
		v.validateMetadata(metadata)
	}

	// Проверка spec - обязательное поле, должно быть объектом
	if spec, ok := fields["spec"]; !ok {
		v.addError(0, "spec is required")
	} else {
		// Валидируем содержимое spec
		v.validateSpec(spec)
	}
}

// validateMetadata - валидация секции metadata
func (v *Validator) validateMetadata(node *yaml.Node) {
	fields := v.parseMap(node)

	// name - обязательное поле, не должно быть пустым
	if name, ok := fields["name"]; !ok {
		// Поле name отсутствует
		v.addError(0, "metadata.name is required")
	} else if strings.TrimSpace(name.Value) == "" {
		// Поле name есть, но это пустая строка (или только пробелы)
		v.addError(name.Line, "name is required")
	}

	// namespace - необязательное поле, проверка не требуется
	// labels - необязательное поле, проверка не требуется
}

// validateSpec - валидация секции spec
func (v *Validator) validateSpec(node *yaml.Node) {
	fields := v.parseMap(node)

	// os - необязательное поле, но если есть, должно быть linux или windows
	if osNode, ok := fields["os"]; ok {
		if osNode.Value != "linux" && osNode.Value != "windows" {
			v.addError(osNode.Line, "os has unsupported value '"+osNode.Value+"'")
		}
	}

	// containers - обязательное поле, должен быть массив контейнеров
	if containers, ok := fields["containers"]; !ok {
		v.addError(0, "spec.containers is required")
	} else if containers.Content != nil {
		// Проходим по каждому контейнеру в массиве
		for _, container := range containers.Content {
			// Проверка что элемент не nil (защита от паники)
			if container != nil {
				v.validateContainer(container)
			}
		}
	}
}

// validateContainer - валидация одного контейнера
func (v *Validator) validateContainer(node *yaml.Node) {
	fields := v.parseMap(node)

	// name - обязательное поле, формат snake_case
	if name, ok := fields["name"]; !ok {
		// Поле отсутствует
		v.addError(0, "containers.name is required")
	} else if strings.TrimSpace(name.Value) == "" {
		// Пустое имя - это тоже ошибка
		v.addError(name.Line, "name is required")
	} else if !snakeCaseRegex.MatchString(name.Value) {
		// Имя не соответствует формату snake_case
		v.addError(name.Line, "containers.name has invalid format '"+name.Value+"'")
	}

	// image - обязательное поле, должен быть из домена registry.bigbrother.io с тегом
	if image, ok := fields["image"]; !ok {
		v.addError(0, "containers.image is required")
	} else if !imageRegex.MatchString(image.Value) {
		// Образ не соответствует требуемому формату
		v.addError(image.Line, "containers.image has invalid format '"+image.Value+"'")
	}

	// ports - необязательное поле, массив портов
	if ports, ok := fields["ports"]; ok && ports.Content != nil {
		for _, port := range ports.Content {
			if port != nil {
				v.validatePort(port)
			}
		}
	}

	// readinessProbe - необязательное поле
	if probe, ok := fields["readinessProbe"]; ok {
		v.validateProbe(probe)
	}

	// livenessProbe - необязательное поле
	if probe, ok := fields["livenessProbe"]; ok {
		v.validateProbe(probe)
	}

	// resources - обязательное поле
	if resources, ok := fields["resources"]; !ok {
		v.addError(0, "containers.resources is required")
	} else {
		v.validateResources(resources)
	}
}

// validatePort - валидация порта контейнера
func (v *Validator) validatePort(node *yaml.Node) {
	fields := v.parseMap(node)

	// containerPort - должен быть целым числом в диапазоне 0 < port < 65536
	if containerPort, ok := fields["containerPort"]; ok {
		// Парсим как int64 для корректной обработки больших и отрицательных чисел
		portNum, err := strconv.ParseInt(containerPort.Value, 10, 64)
		if err != nil {
			// Не удалось распарсить как число
			v.addError(containerPort.Line, "containerPort must be int")
		} else if portNum <= 0 || portNum >= 65536 {
			// Число за пределами допустимого диапазона
			v.addError(containerPort.Line, "containerPort value out of range")
		}
	}

	// protocol - необязательное поле, если есть - должно быть TCP или UDP
	if protocol, ok := fields["protocol"]; ok {
		if protocol.Value != "TCP" && protocol.Value != "UDP" {
			v.addError(protocol.Line, "protocol has unsupported value '"+protocol.Value+"'")
		}
	}
}

// validateProbe - валидация probe (readinessProbe или livenessProbe)
func (v *Validator) validateProbe(node *yaml.Node) {
	fields := v.parseMap(node)

	// httpGet - обязательное поле для probe
	if httpGet, ok := fields["httpGet"]; ok {
		v.validateHTTPGet(httpGet)
	}
}

// validateHTTPGet - валидация httpGet секции в probe
func (v *Validator) validateHTTPGet(node *yaml.Node) {
	fields := v.parseMap(node)

	// path - должен быть абсолютным путем (начинается с /)
	if path, ok := fields["path"]; ok {
		if !absolutePathRegex.MatchString(path.Value) {
			v.addError(path.Line, "path has invalid format '"+path.Value+"'")
		}
	}

	// port - должен быть целым числом в диапазоне 0 < port < 65536
	if port, ok := fields["port"]; ok {
		portNum, err := strconv.ParseInt(port.Value, 10, 64)
		if err != nil {
			v.addError(port.Line, "port must be int")
		} else if portNum <= 0 || portNum >= 65536 {
			v.addError(port.Line, "port value out of range")
		}
	}
}

// validateResources - валидация секции resources
func (v *Validator) validateResources(node *yaml.Node) {
	fields := v.parseMap(node)

	// limits - необязательное поле, максимальные ресурсы
	if limits, ok := fields["limits"]; ok {
		v.validateResourceList(limits)
	}

	// requests - необязательное поле, минимальные ресурсы
	if requests, ok := fields["requests"]; ok {
		v.validateResourceList(requests)
	}
}

// validateResourceList - валидация списка ресурсов (limits или requests)
func (v *Validator) validateResourceList(node *yaml.Node) {
	fields := v.parseMap(node)

	// cpu - должно быть целым числом (не строкой!)
	if cpu, ok := fields["cpu"]; ok {
		// ВАЖНО: если YAML парсер определил это как строку (тег !!str),
		// значит в файле было "1" вместо 1 - это ошибка
		if cpu.Tag == "!!str" {
			v.addError(cpu.Line, "cpu must be int")
		} else {
			// Дополнительная проверка что это действительно число
			_, err := strconv.Atoi(cpu.Value)
			if err != nil {
				v.addError(cpu.Line, "cpu must be int")
			}
		}
	}

	// memory - должно быть строкой в формате "числоGi/Mi/Ki"
	if memory, ok := fields["memory"]; ok {
		if !memoryRegex.MatchString(memory.Value) {
			v.addError(memory.Line, "memory has invalid format '"+memory.Value+"'")
		}
	}
}

// main - главная функция программы
func main() {
	// Проверка аргументов командной строки
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: yamlvalidator <yaml-file>")
		os.Exit(1)
	}

	// Получаем имя файла из аргументов
	filename := os.Args[1]

	// Чтение содержимого файла
	content, err := os.ReadFile(filename)
	if err != nil {
		// Не удалось прочитать файл (файл не существует, нет прав и т.д.)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Парсинг YAML в структуру Node
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		// Ошибка парсинга YAML (невалидный синтаксис)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Создание валидатора и запуск проверки
	validator := NewValidator(filename)
	validator.Validate(&root)

	// Если есть ошибки валидации - выводим их в stderr и завершаем с кодом 1
	if validator.hasErrors() {
		validator.printErrors()
		os.Exit(1)
	}

	// Если ошибок нет - завершаем с кодом 0 (успешная валидация)
	os.Exit(0)
}
