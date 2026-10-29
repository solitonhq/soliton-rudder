import clsx from 'clsx'
import { THEMES, THEME_META } from '../themes/themes'
import { useSettings } from '../store/settings'

export function ThemeSwitcher({ vertical = false }: { vertical?: boolean }) {
  const theme = useSettings((s) => s.theme)
  const setTheme = useSettings((s) => s.setTheme)
  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      className={clsx('no-drag flex gap-2', vertical && 'flex-col')}
    >
      {THEMES.map((t) => {
        const [bg, accent] = THEME_META[t].swatch
        const active = t === theme
        return (
          <button
            key={t}
            role="radio"
            aria-checked={active}
            title={THEME_META[t].label}
            onClick={() => setTheme(t)}
            className={clsx(
              'size-6 rounded-full border transition-transform hover:scale-110 cursor-pointer',
              active ? 'border-accent ring-2 ring-accent/40' : 'border-border',
            )}
            style={{ background: `linear-gradient(135deg, ${bg} 55%, ${accent} 55%)` }}
          />
        )
      })}
    </div>
  )
}
