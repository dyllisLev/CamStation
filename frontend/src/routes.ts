export type AppEntry = 'viewer' | 'mobile' | 'new' | 'classic'

export function getAppEntryForPath(pathname: string): AppEntry {
  if (pathname === '/viewer') return 'viewer'
  if (pathname === '/mobile') return 'mobile'
  if (pathname === '/new' || pathname.startsWith('/new/')) return 'new'
  return 'classic'
}
