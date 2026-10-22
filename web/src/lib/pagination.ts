export function pagination(params: URLSearchParams, defaultLimit = 25) {
  const integer = (key: string, fallback: number, min: number, max: number) => {
    const raw = params.get(key);
    const value = Number(raw);
    return raw !== null &&
      /^\d+$/.test(raw) &&
      Number.isSafeInteger(value) &&
      value >= min &&
      value <= max
      ? value
      : fallback;
  };
  return { offset: integer('offset', 0, 0, 5000), limit: integer('limit', defaultLimit, 1, 200) };
}
export function pageRange(offset: number, count: number, total: number) {
  return count ? `${offset + 1}–${Math.min(offset + count, total)}` : '0';
}
