import type { Camera } from '../../types'
interface Props { camera: Camera; cameras: Camera[]; cameraIndex: number; onClose: () => void; onNavigate: (i: number) => void; onFullscreen: () => void }
export function MobileCamDetail(_props: Props) { return <div /> }
