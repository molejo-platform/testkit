import { mountWebSocketClient } from "./ws-ui.js";
import { mountRestLab } from "./rest-ui.js";
import { mountGraphQLLab } from "./graphql-ui.js";
import { mountSSELab } from "./sse-ui.js";
import { mountPostgresLab } from "./postgres-ui.js";
import { createTranslator, readPageTranslations } from "./i18n.js";

const page = document.body;
const translate = createTranslator(readPageTranslations(page));
const locale = page.dataset.locale || "en";

document.querySelectorAll("[data-ws-client]").forEach((client) => {
  mountWebSocketClient(client, { locale, translate });
});

document.querySelectorAll("[data-rest-lab]").forEach((lab) => {
  mountRestLab(lab, { translate });
});

document.querySelectorAll("[data-graphql-lab]").forEach((lab) => {
  mountGraphQLLab(lab, { translate });
});

document.querySelectorAll("[data-sse-lab]").forEach((lab) => {
  mountSSELab(lab, { locale, translate });
});

document.querySelectorAll("[data-postgres-lab]").forEach((lab) => {
  mountPostgresLab(lab, { translate });
});
