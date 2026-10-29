# Branding

| File | Use |
|---|---|
| `logo-source.png` | The original artwork as supplied (1024 px). The only file to edit by hand. |
| `logo-1024.png` | Cleaned master: the flat background colour `#05090D`, no stray noise around the artwork. |
| `logo-256.png` | README and documentation. |
| `social-preview.png` | GitHub social preview (1280×640): upload it under *Settings → General → Social preview*. |

The app icon (`build/appicon.png`, `build/windows/icon.ico`) and the in-app mark (`frontend/src/assets/logo.png`) are generated
from `logo-source.png`:

```sh
pip install pillow numpy
python3 scripts/gen_branding.py
```

Please do not stretch, recolour or add effects to the logo. The mark is shown as a rounded dark tile so that it reads the same on
light and dark backgrounds.
