// Use only with a complete list. A server-paginated response must supply its own totals.
export function getClientPage<T>(items: readonly T[], requestedPage: number, pageSize: number) {
  if (!Number.isSafeInteger(pageSize) || pageSize < 1) {
    throw new RangeError("Page size must be a positive safe integer.");
  }

  const totalItems = items.length;
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));
  const currentPage = Number.isSafeInteger(requestedPage) && requestedPage > 0
    ? Math.min(requestedPage, totalPages)
    : 1;
  const start = (currentPage - 1) * pageSize;
  const end = Math.min(start + pageSize, totalItems);

  return {
    items: items.slice(start, end),
    currentPage,
    totalPages,
    totalItems,
    startItem: totalItems === 0 ? 0 : start + 1,
    endItem: end,
  };
}
