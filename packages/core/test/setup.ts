// Node 22.4+ ships its own Web Storage global, and from Node 25 it is present
// but inert unless the process was started with a valid `--localstorage-file`.
// In a jsdom test file that global shadows jsdom's own storage, so any code
// behind `typeof window !== "undefined"` hits `localStorage.setItem is not a
// function`. Same in-memory fallback as packages/views/test/setup.ts.

function createMemoryStorage(): Storage {
  const values = new Map<string, string>();

  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key: string) => values.get(key) ?? null,
    key: (index: number) => Array.from(values.keys())[index] ?? null,
    removeItem: (key: string) => {
      values.delete(key);
    },
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
  };
}

if (typeof globalThis.localStorage?.setItem !== "function") {
  const storage = createMemoryStorage();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: storage,
  });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
  }
}
