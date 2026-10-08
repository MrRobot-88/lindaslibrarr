import { catalog } from "./catalog";

export type Locale = "en";
export type TranslationKey = keyof typeof catalog.en;

export function translate(
  _locale: Locale,
  key: TranslationKey,
  values: Record<string, string | number> = {},
) {
  let result: string = catalog.en[key];
  for (const [name, value] of Object.entries(values))
    result = result.replaceAll(`{${name}}`, String(value));
  return result;
}

export function useTranslation() {
  return {
    locale: "en" as const,
    t: (key: TranslationKey, values?: Record<string, string | number>) =>
      translate("en", key, values),
  };
}