# cheat-terminal

چیت‌شیت‌هات رو از توی ترمینال جست‌وجو کن. یه دیتابیس SQLite سریع و
دسته‌بندی‌شده از دستورها، با پیشنهاد زنده موقع تایپ، یه مرورگر تمام‌صفحه،
و یه CLI کوچیک برای جست‌وجو و ویرایش.

- **۱۴۰۰ دستور** از روز اول: لینوکس، پاورشل، داکر، گیت
- **جست‌وجوی زنده** با پیشنهاد تمام‌متن موقع تایپ
- **مرور بر اساس دسته‌بندی**، باز کردن هر دستور برای دیدن جزئیات کامل
- **کپی** هر دستور به کلیپ‌بورد با یه کلید
- **دستورهای خودت**: اضافه، ویرایش و حذف کن؛ به‌روزرسانی سید هیچ‌وقت بهشون دست نمی‌زنه
- **یه باینری استاتیک، یه فایل دیتابیس** (`cheats.db`)، بدون نصب، بدون شبکه

## نصب

باینری سیستم‌عاملت رو از صفحه [Releases](../../releases) بگیر (لینوکس،
ویندوز، مک)، یا خودت بسازش:

```sh
git clone https://github.com/erffanhub-00/cheat-terminal.git
cd cheat-terminal
go build -o cheat-terminal .
```

برای ساختن Go نسخه 1.25 یا جدیدتر لازمه. خود باینری موقع اجرا به هیچ‌چیز
دیگه‌ای نیاز نداره: نه CGO، نه فایل تنظیمات، نه شبکه.

## استفاده

بدون آرگومان اجراش کن تا مرورگر تعاملی باز بشه:

```sh
./cheat-terminal
```

کلیدها: تایپ برای جست‌وجو، Up/Down برای حرکت، Tab برای عوض کردن دسته،
Enter برای جزئیات، c برای کپی دستور، q برای خروج، Esc برای برگشت،
Ctrl+C برای خروج.

یا از خط فرمان استفاده کن:

```sh
./cheat-terminal docker ps        # چاپ دستورهای مطابق
./cheat-terminal --list           # دسته‌ها با تعداد دستورها
./cheat-terminal --list docker    # همه دستورهای داکر
./cheat-terminal add              # اضافه کردن دستور خودت (با سوال)
./cheat-terminal edit 42          # ویرایش دستور شماره ۴۲
./cheat-terminal delete 42        # حذف دستور شماره ۴۲ (اول می‌پرسه)
./cheat-terminal category add kubernetes
./cheat-terminal --import data/   # ایمپورت فایل‌های سید JSON
./cheat-terminal --help           # همه‌چیز
```

## دیتابیس

اولین اجرا `cheats.db` رو کنار باینری می‌سازه (یا اگه اون پوشه قابل‌نوشتن
نباشه، توی پوشه تنظیمات کاربر) و با دیتای سید پرش می‌کنه: ۱۴۰۰ دستور در ۴
دسته. جاش رو می‌تونی با `--db <path>` یا متغیر محیطی
`CHEAT_TERMINAL_DB` عوض کنی.

## دیتای سید

دستورهای سید توی `data/seed.json` هستن که با `tools/generate-seed.py` از
چیت‌شیت‌های انگلیسی (گیت، لینوکس، پاورشل، داکر) ساخته شدن:

```sh
python3 tools/generate-seed.py
./cheat-terminal --import data/
```

ایمپورت فقط رکوردهای سید رو به‌روز می‌کنه. دستورهای خودت جدا ذخیره
می‌شن (`source = 'user'`) و ایمپورت هیچ‌وقت تغییرشون نمی‌ده یا پاکشون
نمی‌کنه.

## توسعه

```sh
go test ./...   # تست‌های واحد (دیتابیس، جست‌وجو، CRUD، ایمپورت، TUI)
go vet ./...    # بررسی‌های استاتیک
```

CI روی vet و تست‌ها اجرا می‌شه و باینری لینوکس، ویندوز و مک رو
می‌سازه. فایل ورک‌فلو با اسم `ci.yml` توی ریشه ریپو هست؛ ببرش به
`.github/workflows/ci.yml` (اپ گیت‌هابی که برای پوش استفاده می‌شه
نمی‌تونه فایل ورک‌فلو بسازه).

## لایسنس

MIT — ببین [LICENSE](LICENSE).

## Me
[![Telegram](https://img.shields.io/badge/Telegram-erffan__hub-blue?style=for-the-badge&logo=telegram)](https://t.me/erffan_hub)
[![Twitter](https://img.shields.io/badge/Twitter-@Erffanhub__00-000000?style=for-the-badge&logo=x&logoColor=white)](https://x.com/Erffanhub_00)
[![Gist](https://img.shields.io/badge/Gist-Profile-000000?style=for-the-badge&logo=github)](https://gist.github.com/erffanhub-00)
[![Github](https://img.shields.io/badge/Github-Profile-000000?style=for-the-badge&logo=github)](https://github.com/erffanhub-00)
