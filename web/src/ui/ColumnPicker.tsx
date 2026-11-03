import { useI18n, type Key } from "../i18n";
import { columnChoices, type ColumnSet } from "../prefs";
import { useSession } from "../session";
import { Icon } from "./Icon";

/** Lets each user choose the optional columns of a list; saved to preferences. */
export function ColumnPicker({ set }: { set: ColumnSet }) {
  const { t } = useI18n();
  const { prefs, savePrefs } = useSession();
  const chosen = prefs.columns[set];
  const toggle = (c: string, on: boolean) => {
    const next = columnChoices[set].filter((x) => (x === c ? on : chosen.includes(x)));
    void savePrefs({ columns: { ...prefs.columns, [set]: next } });
  };
  return (
    <details className="colpicker">
      <summary className="btn sm ghost">
        <Icon name="sliders" />
        {t("columns.title")}
      </summary>
      <div className="colpicker-menu" role="group" aria-label={t("columns.title")}>
        {columnChoices[set].map((c) => (
          <label key={c} className="check">
            <input type="checkbox" checked={chosen.includes(c)} onChange={(e) => toggle(c, e.target.checked)} />
            {t(`columns.${set}.${c}` as Key)}
          </label>
        ))}
      </div>
    </details>
  );
}

export function useColumns(set: ColumnSet): (c: string) => boolean {
  const { prefs } = useSession();
  return (c) => prefs.columns[set].includes(c);
}
