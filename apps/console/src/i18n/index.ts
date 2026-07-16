// Shared i18next instance, initialized once at module load (before any
// component renders) so the first server render already has translations.
// Static `en` resources keep init synchronous and SSR-safe. The console is
// English-first today; the plumbing mirrors the qeet-id console so more
// locales can be dropped in without touching call sites.
import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import common from "./locales/en/common.json";

if (!i18n.isInitialized) {
  void i18n.use(initReactI18next).init({
    lng: "en",
    fallbackLng: "en",
    defaultNS: "common",
    ns: ["common"],
    resources: {
      en: { common },
    },
    interpolation: { escapeValue: false },
    react: { useSuspense: false },
  });
}

export default i18n;
