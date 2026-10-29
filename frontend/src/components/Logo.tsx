import logo from '../assets/logo.png'

export function Logo({ size = 36 }: { size?: number }) {
  return <img src={logo} width={size} height={size} alt="Soliton Rudder" draggable={false} />
}
