import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const { createTranslator } = await import("../static/i18n.js");
const { formatRelativeTime } = await import("../static/ws-ui.js");

const locales = ["en", "pt-BR", "es-AR"];
const catalogs = Object.fromEntries(locales.map((locale) => [
  locale,
  JSON.parse(readFileSync(new URL(`../locales/${locale}.json`, import.meta.url), "utf8")),
]));

test("interpolates translated UI labels and singular and plural relative time for every locale", () => {
  const now = new Date("2026-08-23T12:00:00Z");

  assert.deepEqual(
    Object.fromEntries(Object.entries(catalogs).map(([locale, catalog]) => {
      const translate = createTranslator(catalog);
      return [locale, {
        brand: translate("brand.name"),
        sent: translate("websocket.sent"),
        minute: formatRelativeTime(new Date("2026-08-23T11:59:00Z"), now, translate),
        hour: formatRelativeTime(new Date("2026-08-23T11:00:00Z"), now, translate),
        day: formatRelativeTime(new Date("2026-08-22T12:00:00Z"), now, translate),
        minutes: formatRelativeTime(new Date("2026-08-23T11:58:00Z"), now, translate),
      }];
    })),
    {
      en: { brand: "Molejo Testkit", sent: "Sent", minute: "1 minute ago", hour: "1 hour ago", day: "1 day ago", minutes: "2 minutes ago" },
      "pt-BR": { brand: "Molejo Testkit", sent: "Enviada", minute: "há 1 minuto", hour: "há 1 hora", day: "há 1 dia", minutes: "há 2 minutos" },
      "es-AR": { brand: "Molejo Testkit", sent: "Enviado", minute: "hace 1 minuto", hour: "hace 1 hora", day: "hace 1 día", minutes: "hace 2 minutos" },
    },
  );
});
