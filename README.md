# ⬡ orcli — OpenRouter Agent CLI

[![Go Version](https://img.shields.io/github/go-mod/go-version/xmavexy-ship-it/orcli-openrouter-client?color=00ADD8)](https://go.dev)
[![License](https://img.shields.io/github/license/xmavexy-ship-it/orcli-openrouter-client?color=blue)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20macos-lightgrey)]()
[![Blazing Fast](https://img.shields.io/badge/speed-blazing__fast-orange.svg)]()

**orcli** — ультрабыстрый автономный ИИ-агент для терминала на чистом **Go**. Превращает любую LLM с [OpenRouter](https://openrouter.ai) (Gemini 2.5, Claude 4.5, DeepSeek-R1, GPT-4o...) в полноценного инженера, который читает и пишет код, запускает команды, ищет баги и управляет файлами — **прямо в твоём терминале**.

> **Single Binary. Zero Runtime Dependencies. Blazing Fast.**  
> Весит пару мегабайт, запускается мгновенно, работает везде где есть Go.

```
  ╔════════════════════════════════════╗
  ║   ⬡  orcli  —  OpenRouter Agent   ║
  ╚════════════════════════════════════╝

  Model:        google/gemini-2.5-pro
  Auto-approve: off (will ask before each action)

  [~/projects/myapp] ▸ напиши unit-тесты для auth.go

  ◆ Assistant
  Читаю auth.go, потом напишу тесты...

  ┄ tool calls ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
  📖 read_file: auth.go
  ✏️  write_file: auth_test.go
  ⚠  Write 2341 bytes to auth_test.go? [y/N] y
  ✓ Written 2341 bytes
```

---

## 🔥 Ключевые фичи

- 🤖 **ReAct агент** — не просто чат, а полноценный цикл рассуждение → действие → наблюдение. Модель сама решает какие инструменты вызвать и в каком порядке
- 🛠 **Встроенный тулчейн** — `read_file`, `write_file`, `append_file`, `list_dir`, `search_files`, `run_command`, `delete_file`
- 🛡 **Безопасность** — фильтр опасных паттернов (`rm -rf`, `mkfs`, `dd if=` и др.), подтверждение перед каждым действием
- ⚡ **Стриминг** — ответы выводятся в реальном времени, не ждёшь пока модель закончит думать
- 💾 **Сессии** — сохранение и загрузка истории диалога (`/save`, `/load`)
- 🔀 **Любая модель** — переключай модели прямо в чате (`/model`), работает с любой моделью OpenRouter
- 📁 **Контекст директории** — путь в prompt обновляется динамически, `/cd` для навигации

---

## 🚀 Быстрый старт

### Автоматическая установка (Linux / macOS)

```bash
git clone https://github.com/yourusername/orcli
cd orcli
chmod +x install.sh
./install.sh
```

Инсталлер сам:
- определит дистрибутив и архитектуру (x86_64, arm64)
- установит Go если его нет (pacman / apt / dnf / brew / тарбол с go.dev)
- соберёт бинарник с `-ldflags="-s -w"`
- положит в `/usr/local/bin`

Поддерживаемые системы: Arch, Manjaro, Ubuntu, Debian, Fedora, CentOS, RHEL, Alpine, openSUSE, macOS.

### Ручная установка

```bash
# Нужен Go 1.22+
git clone https://github.com/yourusername/orcli
cd orcli
go mod tidy
go build -ldflags="-s -w" -o orcli .
sudo install -m755 orcli /usr/local/bin/
```

---

## ⚙️ Настройка

Получи API ключ на [openrouter.ai/keys](https://openrouter.ai/keys) (есть бесплатные модели).

```bash
# Через команду (сохраняется в ~/.orcli.json):
orcli config --key sk-or-v1-XXXXXXXXXXXXXXXX

# Или через переменную окружения:
export OPENROUTER_API_KEY=sk-or-v1-XXXXXXXXXXXXXXXX
# Добавь в ~/.bashrc или ~/.zshrc чтобы сохранилось
```

Дополнительные опции конфига:

```bash
orcli config --model anthropic/claude-sonnet-4-5   # модель по умолчанию
orcli config --max-tokens 8192                      # лимит токенов
orcli config --system "Отвечай только по-русски"   # системный промпт
orcli config --auto                                 # авто-подтверждение (осторожно!)
```

Конфиг хранится в `~/.orcli.json`, история сессий в `~/.orcli_history.json`.

---

## 💻 Использование

### Интерактивный режим

```bash
orcli
```

### One-shot режим (для скриптов и пайпов)

```bash
# Один вопрос и выход
orcli "что делает эта функция?"

# Пайп
cat main.go | orcli "найди баги в этом коде"
git diff | orcli "напиши commit message"
```

---

## 🎮 Команды в чате

| Команда | Описание |
|---------|----------|
| `/help` | Показать справку |
| `/model <name>` | Сменить модель на лету |
| `/auto` | Включить/выключить авто-подтверждение |
| `/system <msg>` | Установить системный промпт |
| `/clear` | Очистить историю диалога |
| `/history` | Показать историю диалога |
| `/save` | Сохранить сессию на диск |
| `/load` | Загрузить последнюю сессию |
| `/tools` | Список доступных инструментов |
| `/config` | Показать текущие настройки |
| `/cwd` | Текущая директория |
| `/cd <path>` | Сменить директорию |
| `/quit` | Выйти |

---

## 🛠 Инструменты агента

| Инструмент | Описание |
|------------|----------|
| `read_file` | Прочитать файл |
| `write_file` | Создать или перезаписать файл |
| `append_file` | Дописать в конец файла |
| `list_dir` | Список файлов в директории |
| `run_command` | Выполнить команду в shell |
| `delete_file` | Удалить файл |
| `search_files` | Поиск по содержимому файлов (grep) |

---

## 🤖 Популярные модели

```bash
orcli models   # показать список
```

| Модель | Описание |
|--------|----------|
| `google/gemini-2.5-pro` | Лучший общий результат, огромный контекст |
| `anthropic/claude-sonnet-4-5` | Быстрый и умный |
| `anthropic/claude-opus-4-5` | Самый мощный Claude |
| `openai/gpt-4o` | Флагман OpenAI |
| `deepseek/deepseek-r1` | Reasoning модель, очень дёшево |
| `qwen/qwen-2.5-coder-32b-instruct` | Лучший для кода |
| `meta-llama/llama-4-maverick` | Open source, быстрый |

Полный список: [openrouter.ai/models](https://openrouter.ai/models)

---

## 📁 Структура проекта

```
orcli/
├── main.go        # весь исходный код (~600 строк)
├── go.mod         # зависимости
├── go.sum
├── install.sh     # универсальный установщик
├── README.md
└── LICENSE
```

Намеренно один файл — легко читать, легко форкать.

---

## 🔧 Сборка из исходников

```bash
# Обычная сборка
go build -o orcli .

# Оптимизированная (меньше размер)
go build -ldflags="-s -w" -o orcli .

# Кросс-компиляция для Linux на macOS
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o orcli-linux .

# Для ARM (Raspberry Pi, Apple Silicon)
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o orcli-arm64 .
```

---

## 🛡 Безопасность

По умолчанию агент **всегда спрашивает подтверждение** перед записью файлов и выполнением команд. Встроенный фильтр блокирует потенциально опасные паттерны:

```
rm -rf  •  mkfs  •  dd if=  •  chmod -R 777
curl | sh  •  wget | bash  •  fdisk  •  parted
```

Режим `/auto` отключает подтверждения — используй только если доверяешь модели и понимаешь что делаешь.

---

## 📄 Лицензия

MIT — делай что хочешь.

---

<div align="center">
  <sub>Сделано с ♥ и Go • <a href="https://openrouter.ai">OpenRouter</a></sub>
</div>
