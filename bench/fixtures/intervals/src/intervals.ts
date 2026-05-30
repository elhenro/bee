// interval utilities.

export type Interval = [number, number];

/**
 * Merge a list of intervals into the smallest set of non-overlapping intervals.
 *
 * Contract:
 * - Input may be unsorted. Each interval is [start, end] with start <= end.
 * - Intervals that overlap OR merely touch merge into one,
 *   e.g. [1, 2] and [2, 3] become [1, 3].
 * - Fully contained intervals are absorbed, e.g. [1, 10] and [2, 3] -> [1, 10].
 * - Return the result sorted ascending by start.
 * - Empty input returns an empty array. Must not mutate the input array or its
 *   interval tuples.
 */
export function mergeIntervals(intervals: Interval[]): Interval[] {
  throw new Error("not implemented");
}
