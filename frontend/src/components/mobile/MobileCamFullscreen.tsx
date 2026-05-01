import type { Camera } from '../../types'
interface Props { camera: Camera; cameras: Camera[]; cameraIndex: number; onClose: () => void; onNavigate: (i: number) => void }
export function MobileCamFullscreen(_props: Props) { return <div /> }
