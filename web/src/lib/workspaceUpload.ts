import JSZip from 'jszip'

export const MAX_UPLOAD_FILE_BYTES = 3 * 1024 * 1024 // 3MB

export type FilteredFile = File & { webkitRelativePath?: string }

export interface BuildWorkspaceZipResult {
  blob: Blob
  name: string
  included: number
  skippedHidden: number
  skippedLarge: number
  skippedOther: number
}

/** True if any path segment is a hidden file/dir (name starts with '.'). */
export function isHiddenRelativePath(rel: string): boolean {
  const parts = rel.replace(/\\/g, '/').split('/').filter(Boolean)
  return parts.some((p) => p.startsWith('.'))
}

export function shouldSkipRelativePath(rel: string): 'hidden' | 'vendor' | null {
  const norm = rel.replace(/\\/g, '/')
  if (isHiddenRelativePath(norm)) return 'hidden'
  if (
    norm.includes('/node_modules/') ||
    norm.startsWith('node_modules/') ||
    norm.includes('/__MACOSX/') ||
    norm.startsWith('__MACOSX/')
  ) {
    return 'vendor'
  }
  return null
}

/**
 * Filters a webkitdirectory FileList, skips hidden / vendor / oversized files,
 * then builds a zip archive for upload as FormData field "archive".
 */
export async function buildWorkspaceZipFromDirectory(
  files: FileList | FilteredFile[]
): Promise<BuildWorkspaceZipResult> {
  const list = Array.from(files as ArrayLike<FilteredFile>)
  if (list.length === 0) {
    throw new Error('目录为空')
  }

  const firstRel = list[0].webkitRelativePath || list[0].name
  const name = firstRel.split('/')[0] || 'project'

  const zip = new JSZip()
  let included = 0
  let skippedHidden = 0
  let skippedLarge = 0
  let skippedOther = 0

  for (const file of list) {
    const rel = (file.webkitRelativePath || file.name || '').replace(/\\/g, '/')
    if (!rel) {
      skippedOther++
      continue
    }
    const skip = shouldSkipRelativePath(rel)
    if (skip === 'hidden') {
      skippedHidden++
      continue
    }
    if (skip === 'vendor') {
      skippedOther++
      continue
    }
    if (file.size > MAX_UPLOAD_FILE_BYTES) {
      skippedLarge++
      continue
    }
    // Store as file path inside zip (directories are implied).
    zip.file(rel, file)
    included++
    if (included >= 5000) break
  }

  if (included === 0) {
    throw new Error('没有可上传的文件（已过滤隐藏文件、node_modules 及超过 3MB 的文件）')
  }

  const blob = await zip.generateAsync({
    type: 'blob',
    compression: 'DEFLATE',
    compressionOptions: { level: 6 },
  })

  return { blob, name, included, skippedHidden, skippedLarge, skippedOther }
}

export function formatUploadSkipSummary(r: BuildWorkspaceZipResult): string {
  const parts: string[] = []
  if (r.skippedHidden > 0) parts.push(`隐藏 ${r.skippedHidden}`)
  if (r.skippedLarge > 0) parts.push(`>3MB ${r.skippedLarge}`)
  if (r.skippedOther > 0) parts.push(`其它 ${r.skippedOther}`)
  if (parts.length === 0) return ''
  return `，已跳过：${parts.join('、')}`
}
