import { Body, Controller, Delete, Get, Param, Patch, Post, Put, Query, Req, UseGuards } from '@nestjs/common';
import { ApiParam, ApiTags } from '@nestjs/swagger';
import { Throttle } from '@nestjs/throttler';

import type { UserRole, UserStatus } from '@shared/types';

import { CreateUserDto } from './dto/create-user.dto';
import { UpdateUserDto } from './dto/update-user.dto';
import { isAdminLike, resolveRole, resolveStatus } from './authorization/role.helpers';
import { CredentialThrottlerGuard } from './guards/credential-throttler.guard';
import { JwtAuthGuard } from './guards/jwt.guard';
import type { AuthActor } from './types/auth-actor.types';
import type { FavoriteBlogPostsResponse, FindManagementUsersResponse } from './user.service';
import { UserService } from './user.service';

@ApiTags('User')
@Controller('user')
export class UserController {
  constructor(private readonly userService: UserService) {}

  private parseRole(role?: string): UserRole | undefined {
    if (role === 'super-admin' || role === 'admin' || role === 'moderator' || role === 'user') {
      return role;
    }

    return undefined;
  }

  private parseStatus(status?: string): UserStatus | undefined {
    if (status === 'active' || status === 'suspended') return status;
    return undefined;
  }

  private parseSortBy(sortBy?: string): 'username' | 'email' | 'role' | 'status' | 'createdAt' | 'updatedAt' | undefined {
    if (
      sortBy === 'username' ||
      sortBy === 'email' ||
      sortBy === 'role' ||
      sortBy === 'status' ||
      sortBy === 'createdAt' ||
      sortBy === 'updatedAt'
    ) {
      return sortBy;
    }

    return undefined;
  }

  private parseSortDirection(sortDirection?: string): 'asc' | 'desc' | undefined {
    if (sortDirection === 'asc' || sortDirection === 'desc') {
      return sortDirection;
    }

    return undefined;
  }

  private getActorFromRequest(req: {
    user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean };
  }): AuthActor {
    const user = req.user;
    const role = resolveRole({ role: user?.role, isAdmin: user?.isAdmin });
    const status = resolveStatus({ status: user?.status });

    return {
      sub: user?.sub ?? '',
      role,
      status,
      isAdmin: isAdminLike(role),
    };
  }

  @Post()
  @UseGuards(CredentialThrottlerGuard)
  @Throttle({ default: { limit: 5, ttl: 60000 } })
  async create(@Body() createUserDto: CreateUserDto) {
    return this.userService.create(createUserDto);
  }

  @Get()
  @UseGuards(JwtAuthGuard)
  async findAll(@Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } }) {
    return this.userService.findAll(this.getActorFromRequest(req));
  }

  @Get('management')
  @UseGuards(JwtAuthGuard)
  async findManagementUsers(
    @Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } },
    @Query('page') page?: string,
    @Query('pageSize') pageSize?: string,
    @Query('search') search?: string,
    @Query('role') role?: string,
    @Query('status') status?: string,
    @Query('sortBy') sortBy?: string,
    @Query('sortDirection') sortDirection?: string,
  ): Promise<FindManagementUsersResponse> {
    return this.userService.findManagementUsers(this.getActorFromRequest(req), {
      page: page ? Number.parseInt(page, 10) : undefined,
      pageSize: pageSize ? Number.parseInt(pageSize, 10) : undefined,
      search,
      role: this.parseRole(role),
      status: this.parseStatus(status),
      sortBy: this.parseSortBy(sortBy),
      sortDirection: this.parseSortDirection(sortDirection),
    });
  }

  @ApiParam({ name: 'id' })
  @Get(':id')
  @UseGuards(JwtAuthGuard)
  async findOne(
    @Param('id') id: string,
    @Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } },
  ) {
    return this.userService.findOne(id, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Get(':id/favorites')
  @UseGuards(JwtAuthGuard)
  async getFavorites(
    @Param('id') id: string,
    @Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } },
  ) {
    return this.userService.getFavoriteBlogPostIds(id, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Get(':id/favorites/posts')
  @UseGuards(JwtAuthGuard)
  async getFavoritePosts(
    @Param('id') id: string,
    @Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } },
    @Query('pagesize') pageSize?: string,
    @Query('page') page?: string,
  ): Promise<FavoriteBlogPostsResponse> {
    const parsedPageSize = pageSize ? Number.parseInt(pageSize, 10) : undefined;
    const parsedPage = page ? Number.parseInt(page, 10) : undefined;

    return this.userService.getFavoriteBlogPosts(id, this.getActorFromRequest(req), parsedPageSize, parsedPage);
  }

  @ApiParam({ name: 'id' })
  @ApiParam({ name: 'postId' })
  @Put(':id/favorites/:postId')
  @UseGuards(JwtAuthGuard)
  async addFavorite(
    @Param('id') id: string,
    @Param('postId') postId: string,
    @Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } },
  ) {
    return this.userService.addFavoriteBlogPost(id, postId, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @ApiParam({ name: 'postId' })
  @Delete(':id/favorites/:postId')
  @UseGuards(JwtAuthGuard)
  async removeFavorite(
    @Param('id') id: string,
    @Param('postId') postId: string,
    @Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } },
  ) {
    return this.userService.removeFavoriteBlogPost(id, postId, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Patch(':id')
  @UseGuards(JwtAuthGuard)
  async update(
    @Param('id') id: string,
    @Body() updateUserDto: UpdateUserDto,
    @Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } },
  ) {
    return this.userService.update(id, updateUserDto, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Delete(':id')
  @UseGuards(JwtAuthGuard)
  async remove(
    @Param('id') id: string,
    @Req() req: { user?: { sub?: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean } },
  ) {
    return this.userService.remove(id, this.getActorFromRequest(req));
  }
}
