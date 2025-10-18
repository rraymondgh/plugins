import { nanosecond } from "./env";

export namespace pluginmem {
  let mallocCnt = 0;
  let freeCnt = 0;
  let freeRequestCnt = 0;
  let lastCheck:i64 = 0;
  let collectCnt = 0;
  type pinnedMapT = Map<i64, usize>;
  let pinnedMap: pinnedMapT | null = null;

  export function malloc(size: usize): usize {
    mallocCnt += 1;
    if (mallocCnt - freeCnt > 100 && lastCheck < nanosecond()) {
      lastCheck = nanosecond() + 10 ** 7;
      trace(`malloc[${mallocCnt}] and free[${freeCnt}] out of step`);
    }
    const ptr = __new(size, 0);
    __pin(ptr);

    const n = nanosecond();
    if (pinnedMap == null) {
      pinnedMap = new Map<i64, usize>();
    }
    (<pinnedMapT>pinnedMap).set(n, ptr);

    return ptr;
  }

  export function free(ptr: usize): void {
    const n = nanosecond();
    freeRequestCnt += 1;
    const keys = (<pinnedMapT>pinnedMap).keys();
    const ptrs = (<pinnedMapT>pinnedMap).values();
    for (let i = 0; i < keys.length; i++) {
      if (ptrs[i] == ptr || keys[i] < n - 10**4) {
        freeCnt += 1;
        __unpin(ptrs[i]);
        (<pinnedMapT>pinnedMap).delete(keys[i]);
        collectCnt += 1;
      }
    }
    if (collectCnt > 50) {
      collectCnt = 0;
      __collect();
    }
  }
}
