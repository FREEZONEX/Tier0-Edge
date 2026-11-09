import { useEffect, useRef } from 'react';
import { useLocation, useNavigate } from 'react-router';

/**
 * Hook to set dynamic tab name for the current page.
 * Works with the existing tab system's location.state.tabName mechanism.
 *
 * @param name - The name to display in the tab. Pass undefined to skip.
 */
export function useTabName(
  name: string | undefined,
  options?: { displayName?: string; fullName?: string; contextName?: string }
) {
  const location = useLocation();
  const navigate = useNavigate();
  const initialPathRef = useRef(location.pathname);

  useEffect(() => {
    const nextTabName = options?.displayName || name;
    const nextFullName = options?.fullName || nextTabName;
    // Only update tabName if we're still on the same page
    // This prevents updating tabName when navigating away
    if (
      nextTabName &&
      location.pathname === initialPathRef.current &&
      (location.state?.tabName !== nextTabName ||
        location.state?.tabNameFull !== nextFullName ||
        location.state?.tabContextName !== options?.contextName)
    ) {
      navigate(location.pathname + (location.search || ''), {
        state: {
          ...location.state,
          tabName: nextTabName,
          tabNameFull: nextFullName,
          tabContextName: options?.contextName,
        },
        replace: true,
      });
    }
  }, [name, options?.contextName, options?.displayName, options?.fullName]);
}

export default useTabName;
