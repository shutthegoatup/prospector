import type { Messages } from "@/i18n";

// Each language's catalogue is its own chunk, so a page loads only the one it speaks.
const catalogs = import.meta.glob<Messages>("./lang/*.json", { import: "default" });

export async function catalogue(locale: string): Promise<Messages | undefined> {
  return catalogs[`./lang/${locale}.json`]?.();
}
