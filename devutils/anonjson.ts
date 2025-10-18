import { readFileSync, writeFileSync } from "fs";
import { parseArgs } from "node:util";

function maskString(str: any): string {
  if (typeof str !== "string") return str;

  const finalVisibleChars = 6;

  const asterisks = "*".repeat(Math.max(0, str.length - finalVisibleChars));

  return str.substring(0, finalVisibleChars) + asterisks;
}

function anonymizeValue(value: any): any {
  if (value === null) return null;
  if (Array.isArray(value)) return value.map((item) => anonymizeValue(item));

  switch (typeof value) {
    case "number":
      return value;
    case "string":
      return maskString(value);
    case "boolean":
      return value;
    case "object":
      return anonymizeObject(value);
    default:
      return value;
  }
}

function anonymizeObject(obj: any) {
  if (!obj) return obj;
  const result = Array.isArray(obj) ? [] : {};
  for (const key in obj) {
    if (Array.isArray(obj)) {
      result.push(anonymizeValue(obj[key]));
    } else {
      result[key] = anonymizeValue(obj[key]);
    }
  }
  return result;
}

function doAction(filename: string): void {
  let data = JSON.parse(readFileSync(filename, "utf8"));

  const anonData = JSON.stringify(anonymizeObject(data), null, 2);
  writeFileSync(filename, anonData);
}

const args = parseArgs({
  options: { filename: { type: "string", short: "f" } },
});

if (args.values.filename) {
  doAction(args.values.filename);
}
