import { useNavigate } from 'react-router';
import { useState } from 'react';
import { canModifyParentHref } from '@/utils/common';

const useNavigateForIframe = ({ path, replaceCurrent = false }: { path: string; replaceCurrent?: boolean }) => {
  const navigate = useNavigate();
  const [security] = useState<boolean | -1>(() => canModifyParentHref());

  const onClick = () => {
    if (security === -1) {
      return;
    }
    if (!security) {
      if (replaceCurrent) {
        window.location.replace(path);
      } else {
        navigate(path);
      }
    } else {
      if (replaceCurrent) {
        window.parent.location.replace(path);
      } else {
        window.parent.location.href = path;
      }
    }
  };

  return {
    security: security !== -1,
    onClick,
  };
};

export default useNavigateForIframe;
