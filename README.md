# Jorbique VPN

Простой VPN-клиент для **Windows** и **macOS**. Работает только с сервером
Jorbique: нужен личный UUID, который выдаёт владелец сервера.

## Скачать

Последняя версия на странице [релизов](https://github.com/shootingstar55/jorbique-vpn/releases/latest):

- **Windows:** [Jorbique-VPN-Extended-windows.zip](https://github.com/shootingstar55/jorbique-vpn/releases/latest/download/Jorbique-VPN-Extended-windows.zip)
- **macOS** (Apple Silicon и Intel): [Jorbique-VPN-Extended-macOS.pkg](https://github.com/shootingstar55/jorbique-vpn/releases/latest/download/Jorbique-VPN-Extended-macOS.pkg)

## Как подключиться

**Windows**

1. Распакуй архив.
2. Запусти `Jorbique-VPN-Extended.exe` от имени администратора.
3. Введи свой UUID и нажми **Connect**.

**macOS**

1. Открой `.pkg` и установи. Приложение не подписано, поэтому, если macOS
   ругается, открой его правой кнопкой → **Открыть**.
2. Запусти **Jorbique VPN** из «Программ».
3. Введи свой UUID и нажми **Connect**.

Всё остальное (протоколы, обновления ядра) клиент берёт с сервера сам.

## Для разработчиков

Сборка, устройство и релизы описаны в [docs/development.md](docs/development.md).
