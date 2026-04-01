import { Body, Controller, Delete, Get, Param, Patch, Post, Put, Query, Req, UseGuards } from '@nestjs/common';
import { ApiParam, ApiTags } from '@nestjs/swagger';
import { Throttle } from '@nestjs/throttler';

import { CreateUserDto } from './dto/create-user.dto';
import { UpdateUserDto } from './dto/update-user.dto';
import { CredentialThrottlerGuard } from './guards/credential-throttler.guard';
import { JwtAuthGuard } from './guards/jwt.guard';
import type { FavoriteBlogPostsResponse } from './user.service';
import { UserService } from './user.service';

@ApiTags('User')
@Controller('user')
export class UserController {
  constructor(private readonly userService: UserService) {}

  private getActorFromRequest(req: { user?: { sub?: string; isAdmin?: boolean } }): { sub: string; isAdmin: boolean } {
    const user = req.user;

    return {
      sub: user?.sub ?? '',
      isAdmin: Boolean(user?.isAdmin),
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
  async findAll(@Req() req: { user?: { sub?: string; isAdmin?: boolean } }) {
    return this.userService.findAll(this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Get(':id')
  @UseGuards(JwtAuthGuard)
  async findOne(@Param('id') id: string, @Req() req: { user?: { sub?: string; isAdmin?: boolean } }) {
    return this.userService.findOne(id, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Get(':id/favorites')
  @UseGuards(JwtAuthGuard)
  async getFavorites(@Param('id') id: string, @Req() req: { user?: { sub?: string; isAdmin?: boolean } }) {
    return this.userService.getFavoriteBlogPostIds(id, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Get(':id/favorites/posts')
  @UseGuards(JwtAuthGuard)
  async getFavoritePosts(
    @Param('id') id: string,
    @Req() req: { user?: { sub?: string; isAdmin?: boolean } },
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
    @Req() req: { user?: { sub?: string; isAdmin?: boolean } },
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
    @Req() req: { user?: { sub?: string; isAdmin?: boolean } },
  ) {
    return this.userService.removeFavoriteBlogPost(id, postId, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Patch(':id')
  @UseGuards(JwtAuthGuard)
  async update(
    @Param('id') id: string,
    @Body() updateUserDto: UpdateUserDto,
    @Req() req: { user?: { sub?: string; isAdmin?: boolean } },
  ) {
    return this.userService.update(id, updateUserDto, this.getActorFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Delete(':id')
  @UseGuards(JwtAuthGuard)
  async remove(@Param('id') id: string, @Req() req: { user?: { sub?: string; isAdmin?: boolean } }) {
    return this.userService.remove(id, this.getActorFromRequest(req));
  }
}
