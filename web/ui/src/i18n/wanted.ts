export type WantedLocale = "en";

export const wantedText: Record<WantedLocale, Record<string, string>> = {
  en: {
    wanted_intro:
      "Books Lindarr keeps looking for, and keeps at the quality their profile asks for.",
    wanted_search_now: "Search now",
    wanted_explain: "Why?",
    wanted_monitored: "Monitored",
    wanted_state_missing: "Missing",
    wanted_state_upgrade: "Upgrade wanted",
    wanted_state_downloading: "Downloading",
    wanted_state_satisfied: "Satisfied",
    wanted_state_unmonitored: "Unmonitored",
    dry_run: "[dry run]",
    author_checked: "Checked {author}: {summary}",
    author_baseline:
      "baseline recorded ({seen} works), new releases will be picked up from now on",
    author_new: "{count} new",
    author_added: "{count} added to wanted",
  },
};

export function text(
  locale: WantedLocale,
  key: string,
  values: Record<string, string | number> = {},
): string {
  return (wantedText[locale][key] ?? key).replace(
    /\{(\w+)\}/g,
    (_, name: string) => String(values[name] ?? ""),
  );
}
