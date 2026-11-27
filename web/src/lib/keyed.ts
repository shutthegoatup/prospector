import { useState } from "react";

// Held is a value and the key it was made for.
export type Held<T> = { key: readonly unknown[]; value: T };

export function sameKey(a: readonly unknown[], b: readonly unknown[]): boolean {
  return a.length === b.length && a.every((v, i) => Object.is(v, b[i]));
}

// current is what is held, if it was made for this key, else initial's.
export function current<T>(held: Held<T>, key: readonly unknown[], initial: () => T): T {
  return sameKey(held.key, key) ? held.value : initial();
}

// useKeyedState is state that belongs to its key: a page's cursor to the filters it
// was found under, or edits to the data they began from. When the key changes the
// value starts again from initial in that same render, so nothing ever sees the old
// value with the new key, as it would when an effect put it back a render later.
export function useKeyedState<T>(key: readonly unknown[], initial: () => T): [T, (next: T) => void] {
  const [held, setHeld] = useState<Held<T>>(() => ({ key, value: initial() }));
  return [current(held, key, initial), (next: T) => setHeld({ key, value: next })];
}
