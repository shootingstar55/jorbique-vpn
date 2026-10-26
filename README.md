<div align="center">

<img src="assets/app-icon.png" alt="Jorbique VPN" width="144" height="144">

# Jorbique VPN Extended

**Личный VPN в одну кнопку. Без настроек, без выбора протоколов.**

Нажал «Подключить», клиент сам нашёл самое быстрое рабочее подключение.

[![Последний релиз](https://img.shields.io/github/v/release/shootingstar55/jorbique-vpn?style=for-the-badge&label=%D1%80%D0%B5%D0%BB%D0%B8%D0%B7&color=2bb5a0)](https://github.com/shootingstar55/jorbique-vpn/releases/latest)
[![Windows](https://img.shields.io/badge/Windows-10%20%7C%2011-0f6cbd?style=for-the-badge&logo=windows&logoColor=white)](#-windows)
[![macOS](https://img.shields.io/badge/macOS-11%2B%20%C2%B7%20Apple%20Silicon%20%7C%20Intel-1d1d1f?style=for-the-badge&logo=apple&logoColor=white)](#-macos)

<br>

<a href="https://github.com/shootingstar55/jorbique-vpn/releases/latest">
  <img src="https://img.shields.io/badge/%D0%A1%D0%BA%D0%B0%D1%87%D0%B0%D1%82%D1%8C-%D0%B4%D0%BB%D1%8F%20Windows-0f6cbd?style=for-the-badge&logo=windows&logoColor=white" alt="Скачать для Windows" height="40">
</a>
&nbsp;
<a href="https://github.com/shootingstar55/jorbique-vpn/releases/latest">
  <img src="https://img.shields.io/badge/%D0%A1%D0%BA%D0%B0%D1%87%D0%B0%D1%82%D1%8C-%D0%B4%D0%BB%D1%8F%20macOS-1d1d1f?style=for-the-badge&logo=apple&logoColor=white" alt="Скачать для macOS" height="40">
</a>

</div>

---

## ✨ Что умеет

|     |     |
| --- | --- |
| ⚡ **Одна кнопка** | При каждом подключении клиент проверяет все варианты и выбирает самый быстрый. Ничего выбирать руками не нужно. |
| 🛟 **Не теряет связь** | Если основное подключение отвалится, клиент тихо переключится на запасное из уже проверенных. |
| 🧩 **Исключения** | Нужные приложения и сайты можно пустить мимо VPN. |
| 🌗 **Светлая и тёмная тема** | Одна кнопка: авто, светлая, тёмная. |
| 🚀 **Автозапуск** | Включается вместе с системой, если захочешь. |
| 🔒 **Только свой сервер** | Работает с сервером Jorbique по личному ключу (UUID). Чужим без ключа не подключиться. |

## 📦 Скачать

Свежая версия всегда на странице **[последнего релиза](https://github.com/shootingstar55/jorbique-vpn/releases/latest)**, в разделе **Assets**:

| Система | Файл |
| --- | --- |
| 🪟 Windows 10 / 11 | `Jorbique-VPN-Extended-windows-<версия>.zip` |
| 🍏 macOS 11+ (Apple Silicon и Intel) | `Jorbique-VPN-Extended-macOS-<версия>.pkg` |

> [!NOTE]
> Для входа нужен **личный UUID**. Его выдаёт владелец сервера, без него клиент не подключится.

## 🛠 Установка

### 🪟 Windows

1. Распакуй архив в удобную папку.
2. Запусти `Jorbique-VPN-Extended.exe` **от имени администратора**.
3. Введи свой UUID и нажми кнопку подключения.

### 🍏 macOS

1. Открой `.pkg` и пройди установку.
2. Запусти **Jorbique VPN Extended** из «Программ».
3. Введи свой UUID и нажми кнопку подключения.

> [!TIP]
> Приложение не подписано сертификатом Apple. Если macOS не даёт его открыть, нажми на него **правой кнопкой → Открыть**, а затем подтверди.

## 💡 Как это работает

```
 Подключить ──► проверка Naive ──► работает? ──► да ──► подключено через Naive
                                       │
                                       └── нет ──► проверка остальных ──► самый быстрый
                                                   протоколов                 + запасные
```

Ход проверки видно прямо в анимации подключения. UUID запоминается после первого ввода, поменять его можно в настройках клиента.

## ❓ Частые вопросы

<details>
<summary><b>Не подключается, что делать?</b></summary>

<br>

Проверь, что UUID введён без лишних пробелов, и попробуй подключиться ещё раз. Если не помогло, открой **логи** в клиенте и отправь их владельцу сервера.

</details>

<details>
<summary><b>Как обновиться?</b></summary>

<br>

Скачай новую версию со страницы [релизов](https://github.com/shootingstar55/jorbique-vpn/releases/latest) и установи поверх старой. UUID и настройки сохранятся.

</details>

<details>
<summary><b>Можно выбрать протокол вручную?</b></summary>

<br>

Нет, и не нужно: клиент сам выбирает лучший при каждом подключении и держит запасные на случай сбоя.

</details>

---

<div align="center">

🧑‍💻 Сборка, устройство и релизы для разработчиков: **[docs/development.md](docs/development.md)**

<sub>Работает на <a href="https://github.com/shtorm-7/sing-box-extended">sing-box-extended</a> · сделано с 💚 для своих</sub>

</div>
