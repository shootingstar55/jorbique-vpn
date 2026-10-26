# Jorbique VPN Extended

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
2. Запусти **Jorbique VPN Extended** из «Программ».
3. Введи свой UUID и нажми **Connect**.

При каждом **Connect** клиент проверяет подключения и выбирает рабочее автоматически.
Сначала проверяется Naive: если он работает, используется только он. Иначе клиент
сравнивает остальные варианты и выбирает самый быстрый из доступных. Этапы проверки
видны в анимации подключения.

UUID сохраняется после первого ввода. Изменить его можно кнопкой **Set UUID**.
Выбирать протоколы вручную не нужно; исключения приложений и сайтов и автозапуск
остаются доступны.

## Для разработчиков

Сборка, устройство и релизы описаны в [docs/development.md](docs/development.md).
