<?php

namespace App\Enums;

enum OrganizationRole: string
{
    case Admin = 'admin';
    case Operator = 'operator';
}
