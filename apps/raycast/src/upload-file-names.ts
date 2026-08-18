import path from "node:path";

/**
 * Return stable display names for selected files while preserving selection
 * order. Raycast provides absolute paths, so files from different directories
 * can share a basename even though their upload IDs and object keys are unique.
 * Reserve every literal basename before generating suffixes so a real file such
 * as "report (2).txt" is never displaced by a generated duplicate.
 */
export function disambiguateUploadFileNames(filePaths: string[]): string[] {
  const originalNames = filePaths.map((filePath) => path.basename(filePath));
  const reservedOriginals = new Set(originalNames.map(normalizedFileName));
  const used = new Set<string>();

  return originalNames.map((originalName) => {
    const normalizedOriginal = normalizedFileName(originalName);
    if (!used.has(normalizedOriginal)) {
      used.add(normalizedOriginal);
      return originalName;
    }

    const extension = path.extname(originalName);
    const stem = extension ? originalName.slice(0, -extension.length) : originalName;
    for (let suffix = 2; ; suffix += 1) {
      const candidate = `${stem} (${suffix})${extension}`;
      const normalizedCandidate = normalizedFileName(candidate);
      if (!used.has(normalizedCandidate) && !reservedOriginals.has(normalizedCandidate)) {
        used.add(normalizedCandidate);
        return candidate;
      }
    }
  });
}

function normalizedFileName(value: string): string {
  return value.normalize("NFC").toLocaleLowerCase("en-US");
}
