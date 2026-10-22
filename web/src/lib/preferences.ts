// Browser policies can deny storage even when the Storage API is present.
export function readPreference(key: string, fallback: string): string {
  try {
    return localStorage.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}

export function writePreference(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Preferences still work for the current view when persistence is unavailable.
  }
}
