-- Per-tenant regional-language alert-delivery preference (PRD Module 27.5 /
-- P2-G8). Qeet Logs does not localise notifications itself — it delegates
-- multi-channel, multi-language delivery to the sibling Qeet Notify product
-- (domains/notify). This table records the tenant's DEFAULT locale so an alert
-- fired for that tenant is triggered through Qeet Notify with the right language
-- tag (a per-recipient override still wins over this default; see
-- domains/notify.ResolveLocale).
--
-- Same convention as retention/tenant_plans: explicit tenant_id filtering, NO
-- RLS. An absent row means the platform default ('en').
CREATE TABLE notify_prefs (
    tenant_id     UUID        PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    default_locale TEXT       NOT NULL DEFAULT 'en',   -- BCP-47 tag, e.g. en, hi, ta, bn
    updated_at    TIMESTAMPTZ DEFAULT now()
);
