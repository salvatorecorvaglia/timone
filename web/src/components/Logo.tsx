import projectLogo from '../../../resources/timone.svg';

export function Logo() {
  return (
    <span className="logo-mark">
      <img src={projectLogo} alt="" width={30} height={30} />
    </span>
  );
}
